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
