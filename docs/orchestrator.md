# orchestrator

This is the brain. It takes a user message, sends it to the LLM, streams the response, checks if the model wants to call tools, executes them, feeds the results back, and loops until the model is done. It also handles interrupts, follow-ups, event emission, and state management.

Five files. The split follows the same principle as the rest of the project — each file owns one concern.

## event.go — Agent events

The orchestrator has its own event system, separate from the sender's stream events. Sender events are about "what's happening with this one LLM response." Agent events are about "what's happening with the entire agent run."

Ten events, sealed interface, same pattern as everywhere else:

**Lifecycle:** `AgentStartEvent` (the loop has begun), `AgentEndEvent` (the loop is finished, carries the final conversation).

**Turn-level:** `TurnStartEvent` (a new LLM call is about to happen), `TurnEndEvent` (the LLM responded and any tool calls were executed, carries the assistant message and tool results). A "turn" is one round trip: send messages to the LLM, get a response, optionally execute tools. Multiple turns happen when the model keeps calling tools.

**Message-level:** `MessageStartEvent` (a message is being added — could be user, assistant, or tool result), `MessageUpdateEvent` (the assistant message is being streamed — this wraps a sender event, so consumers can get text deltas), `MessageEndEvent` (message is finalized).

**Tool-level:** `ToolExecutionStartEvent` (about to execute a tool, carries the tool name and arguments), `ToolExecutionUpdateEvent` (intermediate output from a long-running tool), `ToolExecutionEndEvent` (tool finished, carries result and error status).

Every event also names the agent that emitted it. Each event struct embeds a `Source`, and `AgentID()` reads it. The loop builds events without one, and `process_event` stamps the agent's ID on each event before it updates state and notifies listeners. An agent's ID comes from `WithID`; without it, `New` assigns a unique `agent-N`. The ID lets one listener route the events of several agents running at once: the `nvim` package names each agent after its session directory and sends every event into that agent's own `session.md`, which is also how a subagent's forwarded events will find its file.

The layering matters. A UI subscribes to agent events. When it gets a `MessageUpdateEvent`, it unwraps the sender event inside it to get the text delta and print it. When it gets a `ToolExecutionStartEvent`, it shows which tool is running. When it gets `AgentEndEvent`, it knows the whole thing is done.

`MessageUpdateEvent` wrapping a `sender.Event` is a deliberate design choice. We could re-emit every sender event as a separate agent event type, but that would double the number of event types for no real benefit. The wrapper says "something happened with the current message stream" and the consumer can type-switch on the inner event if it cares about specifics.

## config.go — Configuration and state

### ToolExecutionMode

Two modes: `TOOL_EXECUTION_SEQUENTIAL` and `TOOL_EXECUTION_PARALLEL`. Sequential runs tools one at a time, checking for steering messages between each. Parallel runs them all concurrently. Sequential is the default and the safer choice — it lets the agent respond to interrupts between tools.

### AgentLoopConfig

This is the configuration that the loop runs with. It's not the agent's configuration — it's what gets built from the agent's configuration and passed to the loop function. The separation exists because the loop is a pure function that takes its dependencies as arguments. The Agent struct holds the long-lived configuration; the loop config is assembled fresh for each Prompt() call.

The interesting fields:

- **ConvertToLLM** — a transform applied to messages before they're sent to the LLM. The POC passes messages through as-is. A real system might filter out tool results, summarize old messages, or strip thinking content.
- **TransformContext** — another transform, but with access to the context. This is the hook for context window management. When the conversation is getting too long, this function could truncate or compress it. Nothing implements this yet.
- **GetAPIKey** — resolves the API key at call time rather than baking it into the options. This matters when keys rotate or when different providers need different keys.
- **GetSteeringMessages** / **GetFollowUpMessages** — functions that return queued messages. Steering messages interrupt the loop mid-tool-execution. Follow-up messages continue the loop after it would have stopped. These are how the Agent struct communicates with the running loop.
- **BeforeToolCall** / **AfterToolCall** — hooks that run around tool execution. BeforeToolCall can block a tool (return `Block: true` with a reason). AfterToolCall can override the result, the details, or the error status. This is the extensibility point for things like permission systems, rate limiting, or result sanitization.

### Hook context types

`BeforeToolCallContext` and `AfterToolCallContext` carry everything the hooks might need: the assistant message that triggered the call, the specific tool call, the arguments, and the full agent context (system prompt, messages, tools). `AfterToolCallContext` also has the result and error status.

`BeforeToolCallResult` and `AfterToolCallResult` are the return types. Before-hooks either block or allow. After-hooks can override content, details, or error status — all optional. Using pointer for `IsError` (`*bool`) lets you distinguish "don't change it" (`nil`) from "set it to false."

### AgentContext

The runtime state passed through the loop: system prompt, messages, and tools. This is mutated as the loop runs — messages are appended after each LLM response and after each tool execution.

### AgentState

A snapshot of the agent's current state. Used by the `State()` method to give external code a read-only view. Includes everything: system prompt, model, tools, messages, whether streaming is active, the current stream message, which tool calls are pending, and any error. The `PendingToolCalls` map tracks which tools are currently executing — useful for UI progress indicators.

## agent.go — The Agent struct

The Agent is the public API. You create one with `New()`, configure it with functional options, and then call `Prompt()` to run it.

### Construction

`New(options ...AgentOption)` applies functional options (`WithModel`, `WithTools`, `WithSystemPrompt`, etc.) and returns a configured Agent. Default tool execution mode is sequential. The idle channel starts closed (signaling "not running") so `WaitForIdle()` returns immediately if you call it before the first prompt.

Functional options are the standard Go pattern for optional configuration without a sprawling constructor. Each option is a `func(*Agent)` that sets one field. Clean, composable, extensible.

### Prompt(context, message)

This is where the loop runs. It:

1. Acquires the mutex, opens a new idle channel (signaling "running"), appends the user message to the conversation, releases the mutex.
2. Creates a cancellable context from the input context and stores the cancel function (for `Abort()`).
3. Builds an `AgentContext` and `AgentLoopConfig` from the agent's configuration, wiring in the steering/follow-up dequeuers and hooks.
4. Calls `run_loop()` — the pure function that does the actual work.
5. On return, updates the agent's messages with the final conversation and closes the idle channel (signaling "done").

The deferred cleanup (cancel + close idle channel) ensures these always happen, even if the loop panics or returns an error.

`PromptText(context, string)` is convenience — wraps the string in a `UserMessage` with `TextContent` and calls `Prompt()`.

### Abort()

Reads the stored cancel function under a read lock, calls it. This cancels the context that the loop and all its children (HTTP requests, tool executions) are running under. Everything unwinds.

### Subscribe(listener) -> unsubscribe

Adds a listener function to the list. Returns an unsubscribe function that nils out the listener by index. Using nil instead of removing from the slice avoids index shifting, which would invalidate other unsubscribe closures.

### Steer(message) and FollowUp(message)

Both append to their respective queues under the mutex. The loop checks these queues at specific points — steering is checked between tool executions (in sequential mode) or after all tools complete (in parallel mode). Follow-up is checked after the inner loop exits (when the model stopped calling tools).

Steering is for interrupts: "stop what you're doing and pay attention to this." Follow-up is for continuation: "now that you're done with that, do this too."

### State()

Returns a snapshot under a read lock. The state includes everything a UI might need to render the agent's current situation.

### WaitForIdle()

Reads the idle channel under a read lock, then blocks on it. When the loop finishes, it closes the channel, which unblocks all waiters. This is how external code can synchronously wait for the agent to finish.

### process_event

This is the internal event handler. It updates the agent's state based on the event (set `IsStreaming`, track `PendingToolCalls`, capture `Messages`) and then emits the event to all listeners. The state update happens under the mutex, the emission happens after releasing it — listeners might do I/O and you don't want to hold the lock during that.

## orchestrator.go — The loop

`run_loop` is the core cycle. It's a pure function — all dependencies are passed in as arguments. No global state, no side effects beyond what the event sink and context mutation do.

Two nested loops:

**Outer loop** — runs as long as follow-up messages keep arriving. After the inner loop finishes (the model stopped calling tools), it checks for follow-ups. If there are any, it appends them to the conversation and starts the inner loop again. If there aren't, it breaks.

**Inner loop** — the tool-calling cycle. Each iteration:
1. Emit `TurnStartEvent`
2. Call `stream_assistant_response()` to get the LLM's response
3. If the stop reason is error or aborted, bail
4. Extract tool calls from the response. If there are none, the model is done — emit `TurnEndEvent`, break out of the inner loop
5. Execute the tool calls. Append results to the conversation
6. Check for steering messages. If there are any, append them and continue the inner loop (the model will see the interrupt)
7. If no steering, continue the inner loop (the model will see the tool results and respond)

`stream_assistant_response` does the LLM call:
1. Apply transforms (`TransformContext`, `ConvertToLLM`) if they exist
2. Build the `sender.LLMContext` with tool schemas
3. Resolve the API key
4. Call `sender.Stream()`
5. Range over the event stream, emitting agent events as they arrive
6. When the stream ends (EventDone or EventError), capture the final message
7. Append the final message to the conversation

The event forwarding in step 5 is what bridges sender events to agent events. `EventStart` becomes `MessageStartEvent`. `EventDone`/`EventError` become `MessageEndEvent`. Everything else becomes `MessageUpdateEvent` wrapping the sender event.

`extract_tool_calls` walks the assistant message's content blocks and collects all `ToolCall` instances. Simple filter.

## execute.go — Tool execution

`execute_tool_calls` dispatches to either `execute_sequential` or `execute_parallel` based on the config.

### Sequential execution

Iterates over tool calls one at a time:

1. If steering messages have been queued (from a previous tool in this batch), skip the remaining tools. `skip_tool_call` creates an error result saying "Skipped due to queued user message." This tells the model that some tools didn't run because the user interrupted.
2. Emit `ToolExecutionStartEvent`
3. Find the tool by name. Unknown tool → error result.
4. Run the `BeforeToolCall` hook. If blocked → error result with the block reason.
5. Execute the tool.
6. Run the `AfterToolCall` hook. Apply any overrides (content, details, error status).
7. Build the `ToolResultMessage`, emit events.
8. Check for steering messages. If found, the next iteration will skip remaining tools.

After the loop, return all results and any steering messages that arrived.

### Parallel execution

Three phases:

**Phase 1 — Prepare.** Iterate sequentially, running before-hooks and checking for blocked tools. Build an `allowed` array marking which tools can execute.

**Phase 2 — Execute.** Launch goroutines for all allowed tools concurrently. Each goroutine calls `tool.Execute()` and writes the result into a shared array (no mutex needed — each goroutine writes to its own index). `sync.WaitGroup` waits for all to complete.

**Phase 3 — Finalize.** Iterate in original order, running after-hooks and emitting events for each result. This preserves the order of results, which matters for the conversation — tool results should appear in the same order as the tool calls that triggered them.

The three-phase split is important. Before-hooks run sequentially because they might have side effects or depend on order. Execution runs in parallel because tools are independent (reading files, running commands, searching). After-hooks run sequentially because events must be emitted in order.

Steering messages are only checked once, after all tools complete. In parallel mode, you can't meaningfully interrupt between individual tools because they're all running at the same time.

### Helper functions

`find_tool` — linear scan over the tool slice. Fine for 7 tools. Would need a map if you had hundreds.

`skip_tool_call` / `error_tool_result` — construct error `ToolResultMessage` instances. These are always errors (`IsError: true`) because they represent tools that didn't run successfully, either due to interruption or because the tool was unknown or blocked.
