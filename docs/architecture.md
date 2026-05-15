# Architecture

the-agent is a coding agent written in Go. It talks to Kimi K2.5, streams responses over SSE, and executes tools — read files, run commands, edit code, search things. Pure stdlib HTTP, no SDKs.

The whole thing came from taking pi-mono's architecture (which had two monolithic packages, `pi-ai` and `pi-agent-core`, doing way too much each) and splitting it into five focused packages. Each package owns one entity. Each package has one reason to change.

## The five packages

```
        message    model
         ↑   ↑      ↑
        tool  sender
         ↑      ↑
          orchestrator
```

**message** and **model** are the two roots. They don't import anything internal. They don't know about each other. `message` defines the data that flows through the system — what a user said, what the assistant replied, what a tool returned. `model` defines the LLM being targeted — its endpoint, its limits, its pricing.

**sender** and **tool** sit in the middle. `sender` imports `message` and `model` because it needs to send messages to a model and get messages back. `tool` imports `message` because tool results are expressed as content blocks, and it imports `sender` for the `ToolSchema` wire type. These two don't know about each other.

**orchestrator** sits at the top. It imports everything. It's the only package that sees the full picture: it takes messages, sends them through the sender, gets back responses, extracts tool calls, executes tools, feeds results back, and loops until the model stops calling tools.

Nothing points backwards. No circular dependencies. You can compile bottom-up: message and model first, then sender and tool, then orchestrator.

## Why this split

In pi-mono, `pi-ai` handled both "what is a message" and "how do I talk to an LLM." That meant every time you changed the streaming logic, you risked breaking the message types that the whole system depended on. And `pi-agent-core` handled both "what is a tool" and "how does the agent loop work." Same problem.

The split follows a simple rule: if two things change for different reasons, they should be in different packages. Messages change when the data model evolves. The sender changes when you add a new LLM provider or fix streaming bugs. Tools change when you add new capabilities. The orchestrator changes when you change the agent's behavior — how it loops, when it stops, how it handles interrupts.

## What's not here

This is a POC. There's no TUI, no persistence, no context window management (the hook exists but nothing implements it), no conversation history on disk, no permission system for tools. The `cmd/agent` entry point is a bare stdin loop that prints streaming text. The architecture supports all of those things through hooks and events, but they aren't built yet.

## System dependencies

Two external binaries must be on `$PATH`:
- `rg` (ripgrep) — used by the grep tool
- `fd` — used by the find tool

Both are standalone Rust binaries with zero runtime dependencies. Everything else is pure Go stdlib.

## Build and run

```bash
mage build          # compile all packages
mage test           # run all tests
mage testverbose    # run tests with verbose output

KIMI_API_KEY=sk-... go run ./cmd/agent    # run the POC
```
