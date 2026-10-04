# Vim-native design

the-agent lives inside Neovim. There is no agent UI of its own: the chat is a buffer, the prompt is a buffer, every subagent's transcript is a buffer, and every change the agent makes to code lands in the buffer for that file, while you watch. You don't edit — the agent does. You observe, navigate, search and yank with the keys you already have. Nothing in the interface accepts a key that isn't Vim's, because nothing in the interface is anything other than Vim.

"Locked" means Neovim is not only the agent's face but its hands. File tools don't go around the editor with `os.WriteFile`; they go through Neovim's buffer API. That's what makes edits watchable, undoable, visible to the LSP, and safe against each other.

The core packages — `message`, `model`, `sender`, `orchestrator` — don't change. The `chat` package (Bubble Tea) is replaced, and the file tools are rewritten around buffers.

## Two processes

Neovim starts the Go binary as an RPC job:

```lua
vim.fn.jobstart({ "the-agent", "--nvim" }, { rpc = true })
```

The two speak msgpack-RPC over the job's stdin/stdout. On the Go side, `github.com/neovim/go-client` wraps the pipe in a typed client. The binary lives exactly as long as that Neovim instance and inherits its working directory.

```
Neovim ── Lua plugin (keymaps, commands, picker, highlights, approval prompts)
   │  msgpack-RPC over stdio
Go binary ── orchestrator + sender           (unchanged)
          ── nvim package                    (events → buffer operations; replaces chat/)
          ── vimtool package                 (read/edit/write/filter via buffers, leases)
          ── subagent package                (the task tool)
          ── tool package                    (bash, grep, find, ls — bash now sandboxed)
```

Go is the brain, Neovim is the body. Go drives buffers directly through the API (`nvim_buf_set_text`, extmarks, quickfix). The Lua side stays thin: it owns keymaps and commands, sends prompts to Go, and draws anything that needs your input. Go builds against plain Neovim; LazyVim is a consumer, not a dependency.

## The buffers you see

**Chat.** A `nofile` buffer with filetype `markdown`, so it gets highlighting, `/` search and folding for free. The model's reply streams into it. Thinking goes into a fold you open with `zo`. Tool activity appears as one line per call (`▸ edit server.go`), and subagents as one line each (`▸ explore: find every caller of HandleLogin (running)`).

**Prompt.** A small scratch buffer under the chat. It's the one buffer you type into: edit with full Vim, `<CR>` in normal mode sends it.

**Subagent transcripts.** Each subagent gets its own scratch buffer streaming its conversation. `<CR>` on a subagent line in the chat opens it in a split. A picker (snacks in LazyVim, `vim.ui.select` otherwise) lists every running and finished subagent.

**The follow window.** A dedicated window where the agent travels when it edits. It never takes over the window you're in. It follows one agent at a time — the parent by default — and you switch which.

## Observer mode: your buffers are read-only

You watch; the agent edits. Every project buffer gets `modifiable=false` on load, via a `BufReadPost` autocmd from the plugin. Moving, searching, yanking, `gf` and splits all work; any key that would change text errors.

When the agent writes, Go flips `modifiable` on, applies the edit, and flips it off — all three in a single go-client `Batch`, which Neovim processes atomically, so there is no moment you can type into the buffer. `:AgentObserve off` is the escape hatch for the rare time you want to edit by hand.

## Tools

**`read`** returns the buffer's contents if the file is loaded, otherwise loads it with `bufadd` + `bufload` and returns that. The agent always sees what Neovim sees. It records the buffer's `changedtick` for the staleness check (see concurrency).

**`edit`** finds `old_text` in the buffer and replaces it with `nvim_buf_set_text`, as one undo block — `u` reverts one agent edit. After applying it, it waits briefly for `DiagnosticChanged` and appends any new LSP diagnostics in the edited region to the tool result, so the model learns what it broke without running anything.

**`write`** sets a buffer's full contents (creating it if needed). Used for new files; edits to existing files should prefer `edit`.

**`filter`** runs a text-in, text-out command over a buffer, Vim's classic `:%!cmd` — `gofmt`, `sort`, `sed` expressions. It goes through `nvim_buf_call`, so it is an ordinary buffer edit: leased, undoable, streamed, visible. Formatting can also go through the LSP (`vim.lsp.buf.format`, or conform.nvim in LazyVim).

**`grep` / `find` / `ls`** still run `rg` / `fd` / the filesystem, and `grep` also fills the quickfix list so `]q` walks the hits.

**`bash`** runs commands, but cannot change the project. See below.

**`bash_write`** is the guarded exception for commands that legitimately must write to the project. See below.

### Saving

Disk-reading tools — `bash`, `grep`, `find`, the compiler — see the disk, not buffers. So before any of them runs, Go executes `:wall`, writing every modified buffer. In observer mode the only unsaved changes are the agent's, so this never saves something you didn't mean to.

## bash cannot write: a dedicated Unix user

Everything above protects edits that go through Neovim. A shell command doesn't: `gofmt -w .`, `sed -i` or `git checkout` would rewrite files on disk behind the buffers, the leases and your view. The fix is to make that impossible rather than to detect it.

Every `bash` command runs as an unprivileged user:

```go
exec.CommandContext(ctx, "sudo", "-n", "-u", "_the-agent", "bash", "-c", command)
```

`-n` makes sudo fail immediately instead of hanging on an invisible password prompt. The kernel's ordinary Unix permissions do the sandboxing — no VM, no sandbox profile, nothing Apple-specific, identical on Linux.

`_the-agent` can read the project, run builds and tests, and write only to its own `GOCACHE`, `GOMODCACHE` and `TMPDIR` under its home. It is not an admin and has no sudoers entry; if the model runs `sudo`, it fails like any unprivileged user. The agent never gets sudo — your binary uses it once per command to step *down*.

One-time setup:

1. `sysadminctl -addUser _the-agent` (macOS; `useradd` on Linux).
2. `sudo visudo -f /etc/sudoers.d/the-agent` with:
   ```
   niconiahi ALL=(_the-agent) NOPASSWD: ALL
   ```
   You may switch to `_the-agent` without a password. Nothing toward root.
3. Make sure the project is readable but not writable by `_the-agent`.
4. Create its writable `GOCACHE`, `GOMODCACHE` and `TMPDIR`.

### bash_write

Some commands must write to the project: `go mod tidy` rewrites `go.mod`/`go.sum`, `go generate` creates files. Filters can't replace them. `bash_write` runs as you, unsandboxed, under three conditions: no agent holds any lease, you approve it, and its effects are replayed through Neovim. Before it runs, Go hashes the project's files; after, it diffs, and loads each changed file's new contents into its buffer as one undo block, followed by `:checktime`. You see every change land, and `u` reverts it.

## Streaming

**Chat.** `sender/kimi.go` already emits `EventTextDelta` per chunk. The `nvim` package batches deltas every ~40ms and appends them to the end of the chat buffer in one `Batch` call — per-token RPC round trips would stutter. It only auto-scrolls if your cursor is already at the bottom, so reading earlier text isn't yanked away.

**Edits.** The model streams tool-call arguments as JSON fragments, and the sender already pushes each one as `EventToolCallDelta` (`sender/kimi.go:460`) — today `chat/bridge.go` ignores them. A small partial-JSON reader turns the fragments into "fields complete so far", and the edit plays out in the order the JSON arrives:

1. `path` complete → the follow window opens the file.
2. `old_text` complete → the region is found, scrolled to, and highlighted.
3. `new_text` streaming → drawn as extmark virtual text over the region, so you watch the function change shape line by line.
4. `ToolCallEnd` → if `BeforeToolCall` allows it, the preview is applied as the real edit (one undo block); otherwise the extmarks are cleared and nothing changed.

The preview is never the edit. An aborted, blocked or failed call leaves the buffer untouched. Edits are small and targeted by design — watching a whole file retype itself through `write` is far less readable.

## Subagents

A subagent is a tool. `task` creates a fresh `orchestrator.Agent` — its own messages, system prompt and tool subset — runs `PromptText` with the job description, and returns only the child's final answer. Everything the child read dies with it; the parent's context grows by the summary. That is the point: work split into smaller context windows.

It lives in its own `subagent` package above `orchestrator`, because `tool` can't import `orchestrator` without a cycle. Passing the `Execute` context into the child's `Prompt` makes aborting the parent abort its children. Under `TOOL_EXECUTION_PARALLEL`, several `task` calls in one turn run concurrently.

Rules:

- Children don't get `task`. No recursion.
- Roles fix the tool set. Explorers get `read`, `grep`, `find`, `ls`, `bash` and should be most subagents. Workers also get `edit`, `write`, `filter`.
- The child's system prompt says exactly what to return: findings with file paths and line numbers, or a summary of changes made. Too thin a summary makes the parent redo the work.
- The parent's prompt assigns writing subagents disjoint sets of files.

`AgentEvent`s currently carry no source, so the UI can't tell a child's stream from the parent's. Events gain an agent ID (or `task` subscribes to its child and forwards events tagged with one); the `nvim` package routes each agent's stream into its own transcript buffer.

## Concurrency between agents

Neovim is single-threaded, so two agents can never corrupt a buffer. The danger is stale edits: agent B reads a function, agent A changes the file, B edits on the basis of what it read — and if A's change didn't touch B's exact `old_text`, B's edit applies "successfully" and is wrong. Two mechanisms in `vimtool`, shared by every agent because they all live in one Go process:

**Write leases.** A `map[path]agentID` behind a mutex. An agent takes a file's lease on its first edit and releases it when its task ends. Another agent editing that file gets an immediate tool error — *"server.go is being edited by subagent explore-2; work on other files or finish without it"* — never a wait. Waiting is how deadlocks happen (A holds file 1 wanting file 2, B holds 2 wanting 1).

**Staleness check.** Neovim counts every change to a buffer (`nvim_buf_get_changedtick`). `read` records the count per agent; `edit` rejects if the count moved by someone else's change — *"file changed since you read it, re-read first"*. This covers the gap between reading and taking the lease.

Leases are the safety net; the parent partitioning files is the first line. Extmarks, not stored line numbers, track regions across concurrent edits, since extmarks move with the text.

## Approvals

`BeforeToolCall` runs in a Go goroutine, so it may block. It asks Lua to show a `vim.ui.select` prompt (a snacks float in LazyVim); your answer comes back as an `rpcnotify` and Go waits on a channel for it. Neovim itself never freezes waiting on you. What requires approval — `bash_write` always, everything else configurable — is policy, not mechanism.

## LazyVim

The plugin installs as a lazy.nvim spec in `~/.config/nvim/lua/plugins/the-agent.lua`, with `build` running `go build`, loaded lazily on `:Agent` or a keymap. LazyVim's `<leader>a` is its "ai" group, shared with its AI extras, so the plugin registers its keys with which-key under that group (or another prefix). snacks.nvim provides pickers, floats and notifications when present, with `vim.ui.select` / `vim.notify` fallbacks so plain Neovim configs work too. Eventually it can ship as a LazyVim extra.

## Testing

go-client can launch `nvim --embed --headless` inside `go test`, so `vimtool` is tested against a real Neovim — buffers, undo, extmarks, changedtick — the way `tool/` is tested against real files today. The `_the-agent` sandbox gets an integration test asserting a write to the project fails and a write to its `TMPDIR` succeeds.

## Things that will bite

- **RPC volume.** Every go-client call is a round trip. Batch everything; never call per token.
- **Moving targets.** Regions shift under concurrent edits. Extmarks, never stored line numbers.
- **Diagnostics timing.** LSPs report asynchronously. `edit` waits on `DiagnosticChanged` with a short timeout and reports what it has; it can't promise completeness.
- **Stale disk.** Anything that reads disk must be preceded by `:wall`; anything that writes disk (only `bash_write`) must be followed by replay and `:checktime`.

## Build order

1. **`nvim` package + Lua plugin** — RPC job, chat and prompt buffers, streaming chat, abort. Replaces `chat/`. ~1–2 weeks.
2. **`vimtool`** — buffer-backed `read`/`edit`/`write`/`filter`, observer mode, `:wall` policy, quickfix, diagnostics in results. ~1 week.
3. **Sandboxed `bash` + `bash_write`** — `_the-agent` user, replay through buffers. A few days.
4. **Streaming edit previews** — partial-JSON reader, follow window, extmark preview. A few days.
5. **`subagent`** — `task` tool, agent IDs on events, transcript buffers, picker, leases and staleness check. A few days to a week.

Roughly 3–4 weeks in total.
