# message

This is the shared language of the system. Every other package imports it. It defines three things: what content looks like, what messages look like, and how to track token usage.

It imports nothing internal — only Go stdlib (`time`, `encoding/json`). That's by design. If this package had dependencies on anything else, you'd get circular imports the moment two packages both needed to talk about messages.

## content.go — The atoms

A `Content` is the smallest unit of meaning. An assistant response isn't just "a string" — it's a sequence of content blocks, each with a different type. This matters because when Kimi K2.5 responds, it might think first (reasoning content), then write text, then call a tool — all in one response. Each of those is a different content block.

Four concrete types:

- **TextContent** — the actual text the model produces. Has an optional `TextSignature` field for content verification (Anthropic-style signatures, not used by Kimi but the structure supports it).
- **ThinkingContent** — the model's reasoning trace. Same signature pattern, plus a `Redacted` flag for when the thinking is hidden.
- **ImageContent** — base64-encoded image data with a MIME type. Used for tool results that include screenshots or visual output.
- **ToolCall** — the model asking to use a tool. Has an ID (to correlate with the result), a name, and arguments as `map[string]interface{}`.

The `Content` interface is sealed — it has an unexported `is_content()` marker method, so only types in this package can implement it. This gives you exhaustive type switches. If you switch on `Content` and handle all four types, you've handled everything. No external package can sneak in a fifth type.

`ToolCall` being a content block (not a separate field on `AssistantMessage`) is a deliberate choice. It means the model's response is just a flat list of content blocks in order: think, think, text, tool call, tool call. The position in the list is meaningful — it's the order the model produced them. This makes streaming natural: you append blocks as they arrive.

## message.go — The conversation

A `Message` is what gets stored in the conversation history. Three types:

- **UserMessage** — what the user said. Content blocks and a timestamp.
- **AssistantMessage** — what the model said. This is the richest type. It carries the content blocks, but also metadata about the response: which API was used, which provider, which model, token usage, why the model stopped, and an optional error message. All of this lives on the message itself, not on some separate response object, because the conversation history needs to be self-describing. When you replay a conversation, each assistant message tells you exactly where it came from and what it cost.
- **ToolResultMessage** — the response to a tool call. Has the `ToolCallID` to link it back to the `ToolCall` that triggered it, the tool's name, content blocks with the output, optional `Details` (tool-specific metadata like exit codes), and an `IsError` flag.

Same sealed interface pattern. Three types, unexported marker, exhaustive switches.

The `StopReason` on `AssistantMessage` tells you *why* the model stopped generating:
- `stop` — it's done, natural end of response
- `length` — hit the max token limit
- `tool_use` — it wants to call tools
- `error` — something went wrong
- `aborted` — the user cancelled

This is how the orchestrator knows what to do next. `tool_use` means "execute the tools and loop." `stop` means "we're done." Everything else means "bail."

## usage.go — Counting tokens

`Usage` tracks how many tokens went in and came out. `Cost` tracks what that cost in dollars. Both live here because they're part of the message — every `AssistantMessage` carries its usage.

`ModelCost` is interesting. It's a set of per-million-token prices (input, output, cache read, cache write). It lives in *both* this package and the `model` package. That sounds redundant, but it's the cleanest way to keep `message` independent. The `model` package has its own `ModelCost` on the `Model` struct (configuration). This package has its own `ModelCost` as a parameter to `CalculateCost` (computation). The caller bridges the two — passes the model's cost into the function. No import needed between the packages.

`CalculateCost` is straightforward: multiply token counts by per-million prices, divide by a million, sum it up. The result is a `Cost` with individual breakdowns and a total. It's a pure function — no side effects, no state, just math.
