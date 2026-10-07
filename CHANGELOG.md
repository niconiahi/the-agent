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

The agent's file tools go through Neovim buffers. `read` returns the buffer when it is loaded and clean and the disk otherwise, and records the buffer's `changedtick` per agent. `edit` replaces `old_text` in the buffer as one undo block and saves at once, loading files that aren't open into a buffer first; unmatched `old_text` changes nothing. After an edit, new diagnostics inside the edited region are appended to the result, waiting briefly for an attached LSP to publish. `grep` also fills the quickfix list.

### Package: `vimtool`

**Constants**:
- `QUICKFIX_TITLE = "the-agent grep"` - title of the quickfix list grep fills
- `DIAGNOSTICS_WAIT = 500 * time.Millisecond` - longest an edit waits for an attached LSP to publish diagnostics

**Functions**:
- `Grep(client *neovim.Nvim) tool.Tool` - `tool.GrepTool` with the same result, plus one quickfix entry per hit
- `Read(client *neovim.Nvim) tool.Tool` - buffer-backed read that records `b:the_agent_ticks`
- `Edit(client *neovim.Nvim) tool.Tool` - buffer-backed edit, one undo block, saved, region tracked with an extmark, new diagnostics in the region appended to the result
- `Tools(client *neovim.Nvim) []tool.Tool` - the buffer-backed file tools
- `WithSession(invocation_context context.Context, directory string) context.Context` - the calling agent's session directory for the tools

### Package: `tool`

**Functions**:
- `Numbered(content string, arguments map[string]any) ToolResult` - read's numbered, truncated output
- `Diff(old_text string, new_text string) string` - edit's minus/plus listing (was `generate_diff`)

### Package: `nvim`

**Methods**:
- `run` puts the session directory on the turn's context with `vimtool.WithSession`

### Package: `nvim/nvimtest`

**Functions**:
- `StartWithTools(t *testing.T, config nvim.Config, build func(*neovim.Nvim) []tool.Tool) *Harness` - `Start` with tools bound to the harness's Neovim

### Lua plugin

- `lua/the-agent/buffer.lua` - `read`, `edit` and `release`, each one RPC request; `edit` snapshots the diagnostics on the replaced lines and `release(region, timeout)` returns the new ones in the region

### Integration tests

- `integration/fill_quickfix_from_grep_test.go` - a grep tool call fills the quickfix list with file, line and text per hit, for a directory and a single file, and the model's result is rg's output
- `integration/read_files_through_buffers_test.go`, `integration/edit_files_through_buffers_test.go` - scripted `read` and `edit` calls through `:TASend`; asserts the tool result, buffer, disk, undo and `changedtick`
- `integration/report_edit_diagnostics_test.go` - diagnostics published through `vim.diagnostic.set` and through an in-process fake LSP; only new ones inside the edited region are reported, no LSP means no wait, and a silent LSP costs at most `DIAGNOSTICS_WAIT`
