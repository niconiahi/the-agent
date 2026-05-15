# sender

This is where the system connects to the outside world. The sender talks to LLMs over HTTP, parses streaming responses, and emits events that the rest of the system can consume. It imports `message` (for the data types) and `model` (for the target configuration).

Six files, each with a distinct responsibility.

## event.go — The streaming vocabulary

When the sender streams a response from an LLM, it doesn't just hand you a blob of text at the end. It emits a sequence of typed events as the response arrives, token by token.

The `Event` interface is sealed (unexported `is_event()` marker). Twelve concrete types, organized in triplets:

**Text events:** `EventTextStart`, `EventTextDelta`, `EventTextEnd`. A text block is opening, here's a chunk of text, the text block is complete. The delta is a small string — maybe a word, maybe a few characters. The end event carries the full accumulated text.

**Thinking events:** `EventThinkingStart`, `EventThinkingDelta`, `EventThinkingEnd`. Same triplet pattern for the model's reasoning trace. This is what you see when the model "thinks out loud" before answering.

**Tool call events:** `EventToolCallStart`, `EventToolCallDelta`, `EventToolCallEnd`. The model is requesting a tool call. The deltas carry fragments of the JSON arguments as they stream in. The end event carries the complete, parsed `ToolCall` with its ID, name, and deserialized arguments.

**Lifecycle events:** `EventStart` (response has begun), `EventDone` (response is complete, carries stop reason), `EventError` (something went wrong, carries stop reason — either `"error"` or `"aborted"`).

Every event carries a reference to the partial `AssistantMessage` being built. This means any consumer can look at the current state of the response at any point during streaming. The message is mutated in place as new content arrives — when you get a `TextDelta`, the text has already been appended to the message's content blocks.

The `EventType() string` method on each event is there for identification in logging and debugging. You type-switch on the concrete types for actual logic.

## stream.go — The EventStream

This is the concurrency primitive at the heart of streaming. It's a goroutine + buffered channel pattern.

Three channels:
- `events` — buffered channel of `Event` (capacity 64). The producer (the provider goroutine) pushes events, the consumer (the orchestrator) ranges over it.
- `result` — channel of `*AssistantMessage` (capacity 1). Captures the final message when `EventDone` or `EventError` arrives.
- `done` — `chan struct{}` for signaling that the stream is completely finished.

The API:
- `Push(event)` sends an event into the channel. If the event is `EventDone` or `EventError`, it also writes the final message to the result channel. This is the dual-path capture — events flow through the events channel for streaming consumers, but the final message is also captured separately so you can get it without draining events.
- `Close()` closes both the events channel and the done channel. Only the producer calls this. This is what makes `range stream.Events()` terminate.
- `Events()` returns a receive-only channel. The consumer ranges over this.
- `Result()` blocks until the final message is available and returns it.
- `Wait()` blocks until the stream is closed.

The buffer size of 64 is a pragmatic choice. Too small and the producer blocks frequently waiting for the consumer. Too large and you waste memory. 64 events is about 1-2 seconds of fast streaming, which gives the consumer plenty of breathing room.

The result channel has capacity 1 because there's exactly one final message per stream. The done channel is unbuffered because it's only used for signaling — you close it, and all waiters unblock.

## option.go — StreamOptions

Configuration for a single streaming call. Temperature, max tokens, API key, custom headers, and thinking level.

Temperature and MaxTokens are `*float64` and `*int` (pointers) because they're optional — `nil` means "use the model's default." If they were plain values, you couldn't distinguish "the user set temperature to 0" from "the user didn't set temperature."

ThinkingLevel controls how much reasoning the model does. Four levels from minimal to high. This is model-specific — not every model supports it, and the Kimi provider maps it into the right request format.

## provider.go — The registry

The provider registry is how the sender supports multiple LLM APIs without hardcoding any of them.

A `Provider` is simple: an API type string and a `StreamFunction`. The stream function takes a context, a model, an LLM context (system prompt + messages + tool schemas), and stream options — and returns an `EventStream`. That's the entire contract. Everything a provider needs to know is in those four arguments.

`LLMContext` bundles what the LLM needs to see: the system prompt, the conversation history, and the available tools (as `ToolSchema` — just name, description, and JSON Schema parameters, not the actual execute functions). This is the "what to send" without any "how to send it."

`ToolSchema` exists here, separate from the `Tool` struct in the tool package, because the LLM doesn't need to know how to execute a tool. It just needs the tool's name, description, and parameter schema to decide whether to call it. The sender package defines the wire format; the tool package defines the runtime behavior.

The registry itself is a package-level map. `RegisterProvider` adds to it, `GetProvider` looks up by API type string. Providers register themselves in `init()` functions, so they're available the moment the package is imported. No explicit wiring needed.

## sender.go — The public API

Two functions:

**Stream()** — the primary API. Looks up the provider by the model's API type, delegates to its stream function, returns the EventStream. If no provider is registered for that API type, it returns an error. This is the only place where the registry is consulted.

**Complete()** — convenience for when you don't care about streaming. Calls Stream(), drains all events (ranging over the channel and discarding them), then returns the final message via Result(). Useful for testing or batch operations. The events still flow through the channel — they're just not consumed by anyone useful.

## kimi.go — The Kimi provider

This is the largest file in the sender package and the one doing the real work. It implements the OpenAI-compatible streaming protocol that Kimi K2.5 uses.

### Registration

An `init()` function registers the provider with API type `"openai-completions"`. This means any model with `API: "openai-completions"` gets routed here. Kimi, DeepSeek, any OpenAI-compatible provider — one implementation handles them all.

### Wire types

A set of unexported structs that mirror the OpenAI chat completions JSON format: `openai_message`, `openai_tool_call`, `openai_request`, `openai_chunk`, etc. These are the shapes of JSON going in and coming out over the wire. They're separate from the `message` package types because the wire format is an implementation detail of this provider. The `message` package types are the application's internal representation. The two are related but not identical — for example, the OpenAI format puts tool call arguments as a JSON string, while our `ToolCall` has them as a parsed `map[string]interface{}`.

### Message conversion

Five converter functions (`convert_message`, `convert_user_message`, `convert_assistant_message`, `convert_tool_result_message`, `convert_tool_schema`) translate between our types and the OpenAI wire format.

The user message conversion has a branching path: if the message contains images, it uses the multipart content format (`[{"type": "text", ...}, {"type": "image_url", ...}]`). If it's text-only, it uses the simple string format (`"content": "hello"`). The OpenAI API accepts both, but the multipart format is only needed for images. Sending plain text as a multipart array works but is unnecessarily verbose.

Images are converted to data URIs (`data:image/png;base64,...`) inline in the content. This is the format the OpenAI API expects for base64-encoded images.

### SSE parsing

`parse_sse` reads from an `io.Reader` (the HTTP response body) line by line using a `bufio.Scanner`. It's deliberately simple:

- Skip empty lines and comment lines (starting with `:`)
- Only process lines starting with `data: `
- Strip the prefix and send the payload through a channel
- `[DONE]` is the termination signal

The scanner buffer is set to 1MB (`make([]byte, 1024*1024)`) because SSE data lines can be large — especially when tool call arguments contain a lot of JSON. The default scanner buffer is 64KB, which isn't always enough.

### The streaming state machine

`kimi_stream` is the core function. It returns an `EventStream` immediately and does all the work in a goroutine.

The goroutine:
1. Converts all messages and tools to OpenAI wire format
2. Builds the JSON request body
3. Makes the HTTP POST request to `{baseURL}/chat/completions`
4. Launches `parse_sse` as a separate goroutine reading from the response body
5. Runs a select loop consuming SSE data and watching for context cancellation

The select loop is where the state machine lives. It tracks `current_block` — which type of content is currently being streamed (none, text, thinking, or tool). When the type changes (e.g., reasoning content stops and text content starts), it finalizes the current block (emitting an end event) and starts a new one (emitting a start event). The `content_index` tracks which position in the `AssistantMessage.Content` slice the current block occupies.

Tool calls are trickier than text because the OpenAI streaming format sends them incrementally: first a chunk with the tool call ID and function name, then multiple chunks with argument fragments. The arguments arrive as a JSON string being built piece by piece — `{"pa`, then `th": "`, then `/tmp/fo`, then `o"}`. The state machine accumulates these in `tool_call_args` (keyed by tool index, because the model can call multiple tools simultaneously) and only parses the JSON when the block is finalized.

`finalize_block` does the cleanup when transitioning between block types. For text and thinking, it emits the end event with the full accumulated content. For tool calls, it parses the accumulated JSON arguments, builds the final `ToolCall`, patches it into the assistant message's content, and emits the end event. It then clears the accumulator maps so they're ready for the next tool call block.

### Error handling

Three failure modes:
- **HTTP error** (non-200 status) — reads the error body and emits `EventError` with `STOP_REASON_ERROR`
- **Network/request failure** — checks if the context was cancelled (emit `STOP_REASON_ABORTED`) or if it was a genuine error (emit `STOP_REASON_ERROR`)
- **Context cancellation during streaming** — the select loop catches `invocation_context.Done()`, finalizes whatever block is in progress, and emits `EventError` with `STOP_REASON_ABORTED`

The helper functions `emit_error` and `emit_aborted` construct the error `AssistantMessage` with the right metadata (API, provider, model, timestamp) so the error message is still a properly formed message that can be stored in conversation history.
