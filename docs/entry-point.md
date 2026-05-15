# cmd/agent — The entry point

This is the payoff. Everything the other packages define comes together here in 100 lines.

## main.go

### Setup

1. Read `KIMI_API_KEY` from the environment. If it's not set, print to stderr and exit. No env file parsing, no config system — just one environment variable.

2. Create the model with `model.KimiK25()`. One function call, no configuration.

3. Create all seven tools: read, bash, edit, write, grep, find, ls. Each is a factory call that returns a configured `Tool`. Collected into a slice.

4. Create the agent with functional options. Five options:
   - `WithModel` — the Kimi K2.5 model
   - `WithTools` — all seven tools
   - `WithSystemPrompt` — a one-liner telling the model it's a coding agent
   - `WithStreamOptions` — just the API key
   - `WithConvertToLLM` — identity function (pass messages through unchanged)

The `ConvertToLLM` identity function is worth explaining. In a real agent, this is where you'd filter out old tool results, compress long conversations, or strip metadata. For the POC, the full conversation goes to the model every time. This will eventually hit the context window limit on long conversations, but it works for demonstrating the architecture.

### Event handling

A single `Subscribe` call with a function that handles four event types:

- **MessageUpdateEvent** — unwraps the sender event inside. If it's a text delta, prints it to stdout. If it's a thinking delta, prints it to stderr with ANSI dim formatting (`\033[2m...\033[0m`). This gives you streaming output — text appears character by character as it arrives from the API. Thinking shows up dimmed on stderr so it's visible but clearly separate from the model's actual response.

- **ToolExecutionStartEvent** — prints `[tool: name]` to stderr. You see which tool the model is calling.

- **ToolExecutionEndEvent** — prints `[tool done: name]` or `[tool error: name]` to stderr. You see when it finishes.

- **AgentEndEvent** — prints a newline. Clean separation between the response and the next prompt.

Everything diagnostic goes to stderr, the model's response goes to stdout. This means you could pipe the output somewhere and only get the model's words.

### The REPL loop

A standard read-eval-print loop:

1. Print `> ` as a prompt
2. Read a line from stdin with `bufio.Scanner`
3. Skip empty lines
4. Break on `"exit"` or EOF
5. Call `agent.PromptText(root_context, input)` — this blocks until the agent finishes (all streaming, all tool calls, all loops)
6. If there's an error, print it. If the root context is done (Ctrl+C), break. Otherwise, loop back to step 1.

The root context uses `signal.NotifyContext` with `SIGINT` and `SIGTERM`, so Ctrl+C cleanly cancels whatever's in progress. The cancellation propagates down through the agent, through the loop, through the HTTP request, through the tool execution — everything unwinds.

### What's missing

This is a terminal REPL with no frills. No readline (no history, no cursor movement). No color beyond the dim thinking text. No conversation persistence. No multi-line input. No way to configure the model or tools from the command line.

All of those would be built on top of the same primitives. The agent events provide everything a TUI would need. The conversation is in `agent.State().Messages`. The tool execution progress is in the events. The architecture supports it — this file just doesn't implement it yet.

### The blank import

The `_ "github.com/niconiahi/the-agent/sender"` import exists to ensure the sender package's `init()` function runs, which registers the Kimi provider. Without it, `sender.Stream()` would fail with "no provider registered for API type: openai-completions." The sender package is imported transitively through the orchestrator, so this import is actually redundant — but it makes the dependency explicit. You can see at a glance that this binary needs the sender's provider registration to happen.
