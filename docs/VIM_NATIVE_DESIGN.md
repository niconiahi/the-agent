# Vim-native design

the-agent lives inside Neovim. There is no agent UI of its own: a session is a markdown file, every subagent is another markdown file in a subfolder, and every change the agent makes to code lands in the buffer for that file, while you watch. The session file is the model's context — not a log of it, the context itself. You manage it with the motions you already have: `dap` on a noisy tool result removes it from what the model sees next turn.

Neovim is not only the agent's face but its hands. File tools don't go around the editor with `os.WriteFile`; they go through Neovim's buffer API. That's what makes edits watchable, undoable, visible to the LSP, and safe against each other.

The core packages — `message`, `model`, `sender`, `orchestrator` — don't change. The `chat` package (Bubble Tea) stays until the Neovim frontend works, then it is deleted. The file tools are rewritten around buffers.

## Two processes

Neovim starts the Go binary as an RPC job:

```lua
vim.fn.jobstart({ "the-agent", "--nvim" }, { rpc = true })
```

The two speak msgpack-RPC over the job's stdin/stdout. On the Go side, `github.com/neovim/go-client` wraps the pipe in a typed client. The binary lives exactly as long as that Neovim instance and inherits its working directory.

```
Neovim ── Lua plugin (commands, keymaps, highlights, notifications)
   │  msgpack-RPC over stdio
Go binary ── orchestrator + sender           (unchanged)
          ── session package                 (session.md ↔ messages, repair, ceiling)
          ── nvim package                    (events → buffer operations; replaces chat/)
          ── vimtool package                 (read/edit/write/filter via buffers, leases)
          ── subagent package                (the task tool)
          ── tool package                    (bash_read, bash_write, grep, find, ls)
```

Go is the brain, Neovim is the body. Go drives buffers directly through the API (`nvim_buf_set_text`, extmarks, quickfix). The Lua side stays thin: it owns commands and keymaps and forwards them to Go. Go builds against plain Neovim; LazyVim is a consumer, not a dependency.

## Sessions are files

A session lives in the project, at `.the-agent/sessions/<name>/session.md`, and is committed like any other file. The agent never runs git: you commit sessions yourself, usually in the same commit as the code they produced, so a PR can carry the session that made it and a link to the file on GitHub shares it.

```
.the-agent/
├── system_prompt.md
└── sessions/
    └── refactor-auth/
        ├── session.md
        ├── 01-map-callers/
        │   └── session.md
        ├── 02-rewrite-middleware/
        │   ├── session.md
        │   └── 01-update-tests/
        │       └── session.md
        └── unsaved/
            └── utils.go.2026-10-06T15:10
```

`:TA <name>` creates the session or opens it if it exists. Any name is valid except that `/` becomes `-`, because a nested folder would be indistinguishable from a subagent. Naming is configurable. Resuming a session is opening its `session.md`.

### Format

The loader turns `session.md` into the message array and back, losslessly.

The file starts with a link to the system prompt. Every session — root or subagent — links to the same `.the-agent/system_prompt.md`, which holds the agent's rules, the project's `AGENTS.md` and the skill list. Git versions it together with the sessions. Tool definitions are not in the file; they come from code.

Turns are headings. The loader reads only the role; the rest is for you, and helps decide what to snip:

```markdown
## user · 2026-10-06T14:32

## assistant · kimi-k2.5 · 2026-10-06T14:33 · 1,240 tokens
```

Tool calls, tool results and thinking are fenced blocks with their data in the info string. They fold, `dap` removes them, and GitHub renders them as code blocks. Thinking folds closed by default.

````markdown
```thinking
the handler is registered twice…
```

```tool_call id=tc_3 name=edit ts=2026-10-06T14:33
{"path": "server.go", "old_text": "…", "new_text": "…"}
```

```tool_result id=tc_3 ts=2026-10-06T14:33
edit applied
```
````

Everything that happens gets an ISO 8601 timestamp: turns, tool calls and results, session creation, subagent starts, amendments, sidecars. The model sees them too — the loader sends each one as part of its message — so the file and the context never differ.

Images are saved as files next to `session.md` and referenced by relative path. Base64 in the file would dwarf the text and defeat git's delta compression.

### Editing

You can edit anything in the file. There are no guards in alpha.

The one shape Kimi rejects is a broken tool pair: every assistant `tool_calls` entry needs a `tool` message with the same `tool_call_id`, and the reverse. Kimi's `reasoning_content` carries no signature, so thinking is freely editable. When you delete half of a pair, the loader removes the orphan from the request **and from the file**, so `session.md` always shows exactly what was sent. `u` shows what was removed.

### Sending

You write your message under a `## user` heading at the bottom of `session.md` and run `:TASend`. Every command has a `<Plug>(TA…)` mapping; the plugin ships default keys you can override.

While a turn runs, its `session.md` is locked (`modifiable=false`); you can read, move, search and yank. It unlocks when the turn ends or when you run `:TAAbort`.

### The ceiling

There is no compaction. A session has a token ceiling — fixed, configurable, default 200k, and never above the model's `ContextWindow` (262,144 for `kimi-k2.5`). `:TASend` refuses to send above it and shows the count; you trim the file and send again. The statusline shows the current session's count and turns red near the ceiling.

## The buffers you see

**Session.** `session.md`, filetype `markdown`, a normal file buffer. The model's reply streams into it, batched. It only auto-scrolls if your cursor is already at the bottom.

**Subagents.** Each subagent's `session.md` streams the same way. `gf` on its link in the parent opens it; `<C-o>` brings you back. netrw or oil.nvim is the agent explorer, and `:grep` searches every agent in the tree at once.

**The follow window.** A dedicated window where the agent travels when it edits. It never takes over the window you're in. It follows the session you were last in or last sent from, together with all of its subagents; entering a subagent's file keeps following the session it belongs to. Until you enter or send a session, nothing is followed. Edits from other running sessions don't move it; they raise a short notification instead.

## Tools

**`read`** returns the buffer's contents if the file is loaded and has no unsaved changes of yours, otherwise the file on disk. It records the buffer's `changedtick` for the staleness check.

**`edit`** finds `old_text` in the buffer, replaces it with `nvim_buf_set_text` as one undo block — `u` reverts one agent edit — and writes the buffer to disk immediately. After applying it, it waits briefly for `DiagnosticChanged` and appends any new LSP diagnostics in the edited region to the tool result, so the model learns what it broke without running anything.

**`write`** sets a buffer's full contents (creating it if needed) and saves it. Used for new files; edits to existing files should prefer `edit`.

**`filter`** runs a text-in, text-out command over a buffer, Vim's classic `:%!cmd` — `gofmt`, `sort`, `sed` expressions. It goes through `nvim_buf_call`, so it is an ordinary buffer edit: leased, undoable, streamed, saved.

**`grep` / `find` / `ls`** run `rg` / `fd` / the filesystem, and `grep` also fills the quickfix list so `]q` walks the hits.

**`bash_read`** runs a command that only reads. **`bash_write`** runs a command that must write. See below.

No tool asks for approval.

### Disk and buffers agree

Every agent edit is saved the moment it is applied, so disk-reading tools always see the agent's work and there is never a save step before them. Your edits reach the disk only when you save them.

When the agent needs a file whose buffer has unsaved changes of yours, its edit takes precedence. Go writes your unsaved version to a timestamped sidecar, `.the-agent/sessions/<name>/unsaved/<file>.<ts>`, reloads the buffer from disk, applies the edit, saves, and notifies you with the sidecar's path. Nothing of yours is lost, and no file is ever left half-yours, half-the-agent's.

## The shell: `_the-agent`

A shell command would rewrite files on disk behind the buffers, the leases and your view. So no shell command can write the project. Every command runs as an unprivileged user:

```go
exec.CommandContext(ctx, "sudo", "-n", "-u", "_the-agent", "bash", "-c", command)
```

`-n` makes sudo fail immediately instead of hanging on an invisible password prompt. Ordinary Unix permissions do the sandboxing — no VM, no sandbox profile, nothing external, identical on macOS and Linux. `_the-agent` can read the project, and write only to its own `GOCACHE` and `GOMODCACHE` under its home and to two folders setup gives it inside the project: `.the-agent/tmp` (its `TMPDIR`) and `.the-agent/clone` (its copy of the project). It has no password, its shell is `/usr/bin/false`, and it has no sudoers entry: the agent never gets sudo — your binary uses it once per command to step *down*. It does still have network access.

### Setup

The first `:TA` in a project that isn't set up says so and stops:

```
$ sudo the-agent setup
✓ user _the-agent            (created | exists)
✓ /etc/sudoers.d/the-agent   (written, visudo -c ok | exists)
✓ caches ~_the-agent/{gocache,gomodcache}
✓ ACL read on ~/Documents/repos/the-agent (inherit)
✓ ~/Documents/repos/the-agent/.the-agent/{clone,tmp} owned by _the-agent, full control for niconiahi
✓ search on ~, ~/Documents, ~/Documents/repos
✓ search on the folders holding ~/go/bin/the-agent
✓ check: sudo -n -u _the-agent ls / test -x <binary> / test -w .the-agent/clone, .the-agent/tmp
ready
```

It runs once per project and is idempotent: on the first project it creates the user, the sudoers entry (`niconiahi ALL=(_the-agent) NOPASSWD: ALL`, validated with `visudo -cf` before it is installed) and the caches; after that it grants read on the new project, with inheritable ACLs (`chmod +a` on macOS, `setfacl` on Linux), plus search-only on its parent folders and on the folders holding the-agent's binary (so `_the-agent` can run it), and creates `.the-agent/clone` and `.the-agent/tmp` owned by `_the-agent` with an inheritable full-control ACL for me, so I can delete anything `_the-agent` leaves there. `:TA` seeds `.the-agent/.gitignore` with `/clone/` and `/tmp/`. It refuses `/`, `~` and non-directories, prints every command with `--dry-run`, re-checks access with `--check` — macOS TCC can block another user from `~/Documents` even with correct ACLs, and the check says what to allow in System Settings — and undoes itself with `--uninstall` (one project) or `--uninstall --all`.

### `bash_read`

Runs as `_the-agent` in the real project. Tests, builds, `git log` cost nothing extra. A write fails with "Permission denied", which tells the model to use `bash_write`.

### `bash_write`

For commands that must write the project: `go mod tidy`, `go generate`. The command runs as `_the-agent` in its own persistent copy of the project, and its writes come back as ordinary agent edits:

1. Bring `.the-agent/clone` up to date with a hidden `the-agent sync <project>`, run as `_the-agent` through the same `sudo -n` path. It skips `.the-agent/`, keeps `.git`, copies the files whose size, nanosecond mtime or mode differ, one by one (`clonefile` on macOS, `FICLONE` on Linux, a plain copy otherwise), deletes what the project no longer has, and on Linux restores the ACL mask on what it touched. It is idempotent, so an interrupted sync is finished by the next one. The path is stable because Go mixes the absolute directory into its build cache keys; a new path every time would miss the cache on every build.
2. List the clone, run the command there, list it again. It can't reach the real project, because `_the-agent` can't write it.
3. The difference between the two listings, excluding `.git/` (a status command in a clone always rewrites the index), is what the command did.
4. Apply each changed file as an agent edit: leased, staleness-checked, sidecar for your unsaved changes, saved, undoable. Calls on one project are serialized by a lock.

The clone redirects writes; the Unix user is what blocks them. A command that `cd`s to the project's absolute path, or a tool with that path baked in, would otherwise write the real project directly.

One mechanism on every Unix. A bubblewrap overlay was planned for Linux and dropped: overlayfs checks writes against the original files' owner, so `_the-agent` could never modify an overlay of files it doesn't own. The clone lives in the project rather than in `_the-agent`'s home so that a copy-on-write clone stays on the project's filesystem and so that I own the way out: the inherited ACL lets me delete it without sudo.

Measured on an M1 Max: cloning costs about 10 µs per file and the listing about as much again. With the persistent clone, later syncs cost one stat walk plus the changed files. On ext4 the first sync is a full copy.

As built (`clone/` and `tool/clone.go`, see `docs/clone.md` and `docs/tool.md`): `bash_write` holds a lock per project, and every step runs as `_the-agent` with its results streamed back over stdout, so my process never reads the clone itself. The listing before and after the command covers inode, size, nanosecond mtime and mode. Symbolic links and binary files are reported to the model, not applied, and the next sync removes them from the clone. On Linux the sync keeps every clone folder's group bits, which are its ACL mask, at `rwx`; files keep the source's mode, which is safe because deleting a file only needs rights on its folder.

## Streaming

**Sessions.** `sender/kimi.go` emits `EventTextDelta` per chunk. The `nvim` package batches deltas every ~40ms and appends them to the end of the session's `session.md` in one `Batch` call — per-token RPC round trips would stutter.

**Edits.** The model streams tool-call arguments as JSON fragments, and the sender already pushes each one as `EventToolCallDelta` (`sender/kimi.go:460`). A small partial-JSON reader turns the fragments into "fields complete so far", and the edit plays out in the order the JSON arrives:

1. `path` complete → the follow window opens the file.
2. `old_text` complete → the region is found, scrolled to, and highlighted.
3. `new_text` streaming → drawn as extmark virtual text over the region, so you watch the function change shape line by line.
4. `ToolCallEnd` → the preview is applied as the real edit (one undo block) and saved; if the call is aborted or fails, the extmarks are cleared and nothing changed.

The preview is never the edit. Edits are small and targeted by design — watching a whole file retype itself through `write` is far less readable.

## Subagents

A subagent is a tool. `task` creates a fresh `orchestrator.Agent` — its own messages and tool subset — in a new numbered subfolder (`02-rewrite-middleware/`) with its own `session.md`, linked to the same `system_prompt.md`. The job description becomes the child's first `## user` message, which is where role instructions live: what to do, and exactly what to return — findings with file paths and line numbers, or a summary of changes made. Too thin a report makes the parent redo the work.

The child runs, and only its final report comes back. Everything the child read stays in its own file; the parent's context grows by the report. That is the point: work split into smaller context windows, which is how a low ceiling stays livable.

The parent's `session.md` holds the call, a link to the child and a copy of the report:

````markdown
```tool_call id=tc_5 name=task ts=2026-10-06T14:40
{"role": "worker", "job": "rewrite the auth middleware"}
```
[02-rewrite-middleware](02-rewrite-middleware/session.md)
```tool_result id=tc_5 ts=2026-10-06T14:52
Changed middleware.go:40-88 …
```
````

You can `:TASend` in a child's `session.md` to continue it ("you missed `logout.go`"). When that turn ends, Go replaces the report in the direct parent's tool result with the child's new final answer and marks it: ```` ```tool_result id=tc_5 amended=2026-10-06T15:10 ````. If you already deleted that block, there is nothing to replace.

Rules:

- Subagents can use `task` up to a configurable depth limit, default 3. An agent at the limit doesn't get `task`.
- Roles fix the tool set. Explorers get `read`, `grep`, `find`, `ls`, `bash_read` and should be most subagents. Workers also get `edit`, `write`, `filter`, `bash_write`.
- The parent's prompt assigns writing subagents disjoint sets of files.

It lives in its own `subagent` package above `orchestrator`, because `tool` can't import `orchestrator` without a cycle. Passing the `Execute` context into the child's `Prompt` makes aborting the parent abort its children. Under `TOOL_EXECUTION_PARALLEL`, several `task` calls in one turn run concurrently.

Every `AgentEvent` carries the ID of the agent that emitted it (`AgentID()`, set with `orchestrator.WithID`). The `nvim` package names each agent after its session directory and routes each event to that agent's stream, so each agent writes only into its own `session.md`. `task` subscribes to its child and forwards the child's events, still tagged with the child's ID, so they land in the child's file.

## Concurrency

Several root sessions can run at once in one Neovim, each with its own subagents. Neovim is single-threaded, so no two agents can corrupt a buffer. The danger is stale edits, and three mechanisms in `vimtool` cover it, shared by every agent because they all live in one Go process:

**Write leases.** A `map[path]agentID` behind a mutex. An agent takes a file's lease on its first edit and releases it when its task ends. Another agent editing that file gets an immediate tool error — *"server.go is being edited by subagent explore-2; work on other files or finish without it"* — never a wait. Waiting is how deadlocks happen.

**Locks.** What is in use is locked, and nothing else: the `session.md` of every running session, and every file an agent holds a lease on. Go owns the state; Neovim enforces it with `modifiable=false`, flipped in the same atomic `Batch` as the edit, so there is no moment you can type into a leased buffer. Files an agent only read stay open to you.

**Staleness check.** Neovim counts every change to a buffer (`nvim_buf_get_changedtick`). `read` records the count per agent; `edit` rejects if the count moved by someone else's change — another agent's or yours — with *"file changed since you read it, re-read first"*.

Leases and locks live in one Go process's memory. A second Neovim on the same project has its own process and can't see them; not doing that is your responsibility. Extmarks, not stored line numbers, track regions across concurrent edits, since extmarks move with the text.

## Commands

| Command | `<Plug>` | Does |
|---|---|---|
| `:TA <name>` | `<Plug>(TA)` | Create or open a session |
| `:TASend` | `<Plug>(TASend)` | Send the current `session.md` |
| `:TAAbort` | `<Plug>(TAAbort)` | Abort the current session's turn |

## LazyVim

The plugin installs as a lazy.nvim spec in `~/.config/nvim/lua/plugins/the-agent.lua`, with `build` running `go build`, loaded lazily on its commands. LazyVim's `<leader>a` is its "ai" group, so default keys register with which-key under that group. snacks.nvim provides notifications when present, with `vim.notify` as the fallback. Eventually it can ship as a LazyVim extra.

## Testing

go-client can launch `nvim --embed --headless` inside `go test`, so `vimtool` is tested against a real Neovim — buffers, undo, extmarks, changedtick, sidecars — the way `tool/` is tested against real files today. The `session` package gets round-trip tests: `session.md` → messages → `session.md` is identity, and orphans are removed from both. The `_the-agent` sandbox gets an integration test asserting a write to the project fails, a write to its `TMPDIR` succeeds, and a `bash_write` change comes back as an edit.

## Things that will bite

- **RPC volume.** Every go-client call is a round trip. Batch everything; never call per token.
- **Moving targets.** Regions shift under concurrent edits. Extmarks, never stored line numbers.
- **Diagnostics timing.** LSPs report asynchronously. `edit` waits on `DiagnosticChanged` with a short timeout and reports what it has; it can't promise completeness.
- **Clone cost.** The first sync grows with file count — about 1 s at 100k files on macOS, a full copy on ext4, which has no reflinks. Later syncs only stat and copy what changed.
- **ACL mask on Linux.** A copied file's ACL mask comes from the mode the copier asks for, so a 0644 source leaves me `r--` on the copy and unable to delete it. Only what `_the-agent` creates natively inherits full control; the sync must restore the mask on what it copies.
- **macOS TCC.** It can deny `_the-agent` access to `~/Documents` regardless of ACLs. `setup --check` catches it.

## Build order

1. **`nvim` package + Lua plugin + `session` package** — RPC job, `:TA`/`:TASend`/`:TAAbort`, `session.md` format and loader, repair, ceiling, streaming into the file, abort. Replaces `chat/`, which is deleted once this works.
2. **`vimtool`** — buffer-backed `read`/`edit`/`write`/`filter`, save-on-edit, sidecars, quickfix, diagnostics in results.
3. **`_the-agent`** — `the-agent setup`, `bash_read`, `bash_write` with clone and replay.
4. **Streaming edit previews** — partial-JSON reader, follow window, extmark preview.
5. **`subagent`** — `task` tool, subfolders, depth limit, agent IDs on events, amendments, leases, locks and staleness check across parallel sessions.
