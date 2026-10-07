# Changelog

## Milestone: Sessions as files

the-agent runs only inside Neovim. A session is `.the-agent/sessions/<name>/session.md`, the model's whole context: `:TA <name>` opens or creates it, `:TASend` streams the reply into it (locked while the turn runs), `:TAAbort` stops a turn. Every turn, tool call, tool result and the session's creation carry an ISO 8601 timestamp that the model also sees. Orphaned tool calls or results are dropped from the request and removed from the file (one `u` brings them back), images live beside the file, and a token ceiling refuses oversized sends. The Bubble Tea `chat` package is gone.

### Package: `session`

**Types**:
- `Session` - a parsed session.md that renders back byte for byte

**Constants**:
- `ROLE_USER = "user"`, `ROLE_ASSISTANT = "assistant"` - turn roles read from `## ` headings
- `TIMESTAMP_FORMAT = time.RFC3339` - every timestamp in a session
- `BLOCK_THINKING`, `BLOCK_TOOL_CALL`, `BLOCK_TOOL_RESULT` - fenced block kinds in assistant turns
- `SYSTEM_PROMPT_LINK = "[system_prompt.md](../../system_prompt.md)"` - first line of a new session
- `CREATED_PREFIX = "created · "` - the session-creation line
- `DEFAULT_CEILING = 200_000`, `BYTES_PER_TOKEN = 4`, `IMAGE_TOKENS = 1_000` - token estimate and ceiling

**Functions**:
- `Parse(text string) (*Session, error)` - reads a session.md
- `New(at time.Time) string` - the text of a new session: link, creation line, bare `## user`
- `Ceiling(configured int, context_window int) int` - the effective ceiling
- `EstimateTokens(system_prompt string, messages []message.Message) int` - the estimate that guards a send
- `FormatCount(value int) string`, `FormatTokens(value int) string` - counts as written in headings
- `ThinkingRanges(text string) [][2]int` - 1-based line ranges of thinking blocks, for folding
- `ThinkingBlock`, `ToolCallBlock`, `ToolResultBlock` - one block rendered at a time, for streaming
- `SaveImage(directory string, image message.ImageContent) (string, error)` - writes an image beside the session, returns its reference
- `LoadImages(messages []message.Message, directory string) ([]message.Message, error)` - attaches the images references point at

**Methods**:
- `(*Session) Messages() []message.Message` - what the model receives, orphans left out
- `(*Session) Render() string` - the file
- `(*Session) Repair() bool` - removes orphaned tool calls and results
- `(*Session) StampLastUser(now time.Time) bool` - timestamps a bare last `## user`
- `(*Session) AppendAssistant`, `AppendAssistantMessage`, `AppendToolResult`, `AppendUser` - add turns and blocks
- `(*Session) SystemPromptLink() (string, bool)` - the linked system prompt file
- `(*Session) SystemPrompt(linked string) string` - the linked prompt plus the creation line

### Package: `nvim`

**Types**:
- `Config` - model, tools, project directory, clock and readiness check for `Attach`

**Constants**:
- `METHOD_OPEN`, `METHOD_SEND`, `METHOD_ABORT`, `METHOD_COUNT`, `METHOD_THINKING` - RPC methods the Lua plugin calls
- `FLUSH_INTERVAL = 40ms` - streamed text batching
- `ABORT_WAIT = 5s` - how long `:TAAbort` waits for the turn to unlock
- `NEAR_CEILING = 0.9` - statusline turns red from here
- `SYSTEM_PROMPT_GUIDE` - appended to the seeded system_prompt.md: where AGENTS.md and the skill list go

**Functions**:
- `Attach(client *neovim.Nvim, config Config) error` - registers the RPC handlers
- `SessionPath(project string, name string) string`, `SystemPromptPath(project string) string` - where files live

**Errors**:
- `ERROR_NOTHING_TO_SEND` - `:TASend` without a message under the last `## user`

### Package: `nvim/nvimtest`

**Types**:
- `Harness` - a headless Neovim with the plugin loaded
- `Provider`, `Reply` - a scripted fake model provider

**Functions**:
- `Start`, `Launch`, `Config`, `Model`, `RegisterProvider`, `Text`, `RepoRoot` - harness entry points

### Package: `message`

**Functions**:
- `ContentOf(current Message) []Content` - any message's content

### Lua plugin

- `plugin/the-agent.lua`, `lua/the-agent/` - `:TA`, `:TASend`, `:TAAbort`, `<Plug>` mappings, default `<leader>a` keys, snacks notifications, a token statusline installed only when you haven't set your own, thinking folds
- `extras/lazy.lua` - lazy.nvim spec that builds the binary

### Integration tests

`integration/` drives a headless Neovim through `nvimtest` and the real binary: opening, sending, streaming, tool turns, orphan repair, the ceiling, the statusline, images, the system prompt, key mappings and the lazy.nvim spec.

## Milestone: Buffer-backed file tools

The agent's file tools go through Neovim buffers. `read` returns the buffer when it is loaded and clean and the disk otherwise, and records the buffer's `changedtick` per agent. `edit` replaces `old_text` in the buffer as one undo block and saves at once, loading files that aren't open into a buffer first; unmatched `old_text` changes nothing. After an edit, new diagnostics inside the edited region are appended to the result, waiting briefly for an attached LSP to publish. `grep` also fills the quickfix list. `write` sets a buffer's whole content, creating the file if needed, and `filter` runs a text-in, text-out command over a buffer like `:%!cmd`; both save at once as one undo block. An agent change to a buffer with my unsaved changes first saves my version to `<session>/unsaved/<path>.<timestamp>`, reloads the buffer from disk, applies the change and notifies me with the sidecar's path; one `u` then reverts only the agent's change, back to the disk version.

### Package: `vimtool`

**Constants**:
- `QUICKFIX_TITLE = "the-agent grep"` - title of the quickfix list grep fills
- `DIAGNOSTICS_WAIT = 500 * time.Millisecond` - longest an edit waits for an attached LSP to publish diagnostics

**Functions**:
- `Grep(client *neovim.Nvim) tool.Tool` - `tool.GrepTool` with the same result, plus one quickfix entry per hit
- `Read(client *neovim.Nvim) tool.Tool` - buffer-backed read that records `b:the_agent_ticks`
- `Edit(client *neovim.Nvim) tool.Tool` - buffer-backed edit, one undo block, saved, region tracked with an extmark, new diagnostics in the region appended to the result
- `Write(client *neovim.Nvim) tool.Tool` - buffer-backed write that creates the file if needed, one undo block, saved
- `Filter(client *neovim.Nvim) tool.Tool` - runs a shell command over a buffer like `:%!cmd`, one undo block, saved; a failing command changes nothing
- `WithSession(invocation_context context.Context, directory string, now func() time.Time) context.Context` - the calling agent's session directory and clock for the tools

### Package: `tool`

**Types**:
- `LineRange{Offset, Limit int}` - which lines a read returns

**Functions**:
- `LineRangeFrom(arguments map[string]any) LineRange` - a read's `offset` and `limit` arguments, defaulting to line 1 and `MAX_READ_LINES`
- `Numbered(content string, line_range LineRange) ToolResult` - read's numbered, truncated output
- `Diff(old_text string, new_text string) string` - edit's minus/plus listing (was `generate_diff`)

### Package: `nvim`

**Methods**:
- `run` puts the session directory and `Config.Now` on the turn's context with `vimtool.WithSession`

### Package: `nvim/nvimtest`

**Functions**:
- `StartWithTools(t *testing.T, config nvim.Config, build func(*neovim.Nvim) []tool.Tool) *Harness` - `Start` with tools bound to the harness's Neovim

### Lua plugin

- `lua/the-agent/buffer.lua` - `read`, `edit`, `write`, `filter` and `close_region`, each one RPC request; edit, write and filter share the sidecar-and-reload step for buffers with unsaved changes, which breaks the undo sequence after the reload; `edit` snapshots the diagnostics on the replaced lines and `close_region(region, timeout)` returns the new ones in the region

### Integration tests

- `integration/vimtool_helpers_test.go` - shared helpers for the vimtool tests: start Neovim with the vimtool tools and scripted tool calls, send a turn, read tool results, buffer lines, ticks, undo and recorded notifications
- `integration/fill_quickfix_from_grep_test.go` - a grep tool call fills the quickfix list with file, line and text per hit, for a directory and a single file, and the model's result is rg's output
- `integration/read_files_through_buffers_test.go`, `integration/edit_files_through_buffers_test.go` - scripted `read` and `edit` calls through `:TASend`; asserts the tool result, buffer, disk, undo and `changedtick`, and the sidecar, notification and one-undo-back-to-disk for a buffer with my unsaved changes
- `integration/write_files_through_buffers_test.go` - `write` creates a file, replaces an open buffer as one undo block, and sets my unsaved changes aside in a sidecar first, after which one undo reverts to the disk version
- `integration/filter_buffers_through_commands_test.go` - `filter` with `sort` and `gofmt` as one saved undo block; a failing command is a tool error that changes nothing
- `integration/report_edit_diagnostics_test.go` - diagnostics published through `vim.diagnostic.set` and through an in-process fake LSP; only new ones inside the edited region are reported, no LSP means no wait, and a silent LSP costs at most `DIAGNOSTICS_WAIT`

## Milestone: _the-agent sandbox

`sudo the-agent setup [project]` prepares a project so the agent's shell commands can run as `_the-agent`. On the first project it creates the user (no password, `/usr/bin/false` shell), writes `/etc/sudoers.d/the-agent` (`<me> ALL=(_the-agent) NOPASSWD: ALL`, installed only after `visudo -cf` accepts the draft) and creates `~_the-agent/{gocache,gomodcache,tmp,clones}`; on every project it grants an inheritable read ACL (`chmod +a` on macOS, `setfacl` on Linux), search on the parent folders up to my home, and records the project in `~_the-agent/projects`. Each step prints `exists` or `created`, so rerunning it changes nothing, and it ends with a check. It refuses `/`, my home or a folder containing it, and anything that isn't a directory. `--dry-run` prints every command without running one and needs no root, `--check` runs `sudo -n -u _the-agent ls <project>` and names Full Disk Access when macOS TCC blocks it, `--uninstall` undoes one project (keeping parent search other projects still need) and `--uninstall --all` removes every project's ACLs, the home, the sudoers file and the user.

### Package: `setup`

**Types**:
- `Host` - the machine setup acts on: system, invoking user and home, working directory, root or not, the shell and where output goes
- `Shell` - runs one `Command` and returns its combined output; the seam tests fake
- `Command{Args, Input}` - one command and its stdin; `String()` is what `--dry-run` prints

**Constants**:
- `USER = "_the-agent"`, `SUDOERS = "/etc/sudoers.d/the-agent"`, `SUDOERS_DRAFT` - the user and its sudoers file (the draft name has a `.`, so sudo ignores it)
- `DARWIN_HOME = "/var/the-agent"`, `LINUX_HOME = "/var/lib/the-agent"` - `~_the-agent`
- `DARWIN_READ`, `DARWIN_SEARCH` - the macOS ACL entries for a project and its parents
- `DARWIN_FIRST_ID = 400`, `DARWIN_LAST_ID = 499` - where the macOS user and group id is picked
- `USAGE` - the setup command line

**Functions**:
- `Run(host Host, arguments []string) error` - `the-agent setup` with its flags and optional project
- `Local(output io.Writer) (Host, error)` - this machine, with `SUDO_USER` as the invoking user when run under sudo

**Errors**:
- `ERROR_NOT_ROOT` - setup or uninstall without root and without `--dry-run`
- `ERROR_ALL_WITHOUT_UNINSTALL` - `--all` alone
- `ERROR_UNSUPPORTED_SYSTEM` - neither macOS nor Linux
- `ERROR_NO_FREE_ID` - no free macOS id between 400 and 499

### Package: `cmd/agent`

- `the-agent setup …` runs `setup.Run` on `setup.Local`, printing to stdout and exiting 1 with the error on stderr

### Integration tests

- `integration/set_up_the_agent_sandbox_test.go` - the built binary's `setup --dry-run` prints the project's ACL step and check without root, and `setup /` is refused with exit status 1
