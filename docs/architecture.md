# Architecture

the-agent is a coding agent written in Go. It talks to Kimi K2.5, streams responses over SSE, and executes tools — read files, run commands, edit code, search things. It runs inside Neovim, and a conversation is a markdown file, `.the-agent/sessions/<name>/session.md`, which is the model's whole context. Pure stdlib HTTP, no LLM SDKs; the only dependency is Neovim's Go client.

The core came from taking pi-mono's architecture (which had two monolithic packages, `pi-ai` and `pi-agent-core`, doing way too much each) and splitting it into five focused packages. Each package owns one entity. Each package has one reason to change. Two more packages sit on top of them for the session files and the Neovim frontend.

## The packages

```
        message    model
         ↑   ↑      ↑
        tool  sender
         ↑      ↑
          orchestrator     session ──→ message
               ↑              ↑
               └──── nvim ────┘
                      ↑    └──→ vimtool ──→ tool
                      │            ↑
                  cmd/agent ───────┘
                      │
                      └──→ setup
```

**message** and **model** are the two roots. They don't import anything internal. They don't know about each other. `message` defines the data that flows through the system — what a user said, what the assistant replied, what a tool returned. `model` defines the LLM being targeted — its endpoint, its limits, its pricing.

**sender** and **tool** sit in the middle. `sender` imports `message` and `model` because it needs to send messages to a model and get messages back. `tool` imports `message` because tool results are expressed as content blocks, and it imports `sender` for the `ToolSchema` wire type. These two don't know about each other.

**orchestrator** imports all of the above. It takes messages, sends them through the sender, gets back responses, extracts tool calls, executes tools, feeds results back, and loops until the model stops calling tools.

**session** owns the `session.md` format and imports only `message`. It parses a file into the messages the model receives, with the file's timestamps in their content and orphaned tool calls or results left out, and renders turns, thinking, tool calls, tool results and image references back byte for byte. It also estimates a session's tokens against its ceiling. It knows nothing about Neovim.

**nvim** is the frontend. It answers the Lua plugin's msgpack-RPC requests (`:TA`, `:TASend`, `:TAAbort`, the statusline count, the thinking folds), runs a fresh orchestrator agent per send with the parsed session as history, and streams the reply into the session buffer, which stays locked while the turn runs. `nvim/nvimtest` is its headless-Neovim harness, used by the tests in `/integration`. The Lua side (`plugin/`, `lua/the-agent/`) stays thin: it forwards commands and applies the edits Go asks for.

**vimtool** holds the tools that need the editor. They wrap or replace `tool` tools and take the Neovim client; `cmd/agent` builds them once the client exists. `read`, `edit`, `write` and `filter` go through the buffer API (in `lua/the-agent/buffer.lua`), so an agent change is one undo block, saved at once and visible to the LSP, and my unsaved changes are set aside in a session sidecar before any agent change touches the buffer. `vimtool.Grep` runs the plain grep and fills the quickfix list with its hits. `nvim` imports it only to put the session directory and clock on the turn's context. It never imports `nvim`.

**setup** is `the-agent setup`, the one-time, root-run preparation that lets shell commands run as the unprivileged `_the-agent` user: the user, its sudoers entry, its caches, and read ACLs on each project. It imports nothing internal and talks to the machine only through a `Shell` that runs commands, which is what `--dry-run` prints and what its tests fake. See `setup.md`.

Nothing points backwards. No circular dependencies. You can compile bottom-up: message and model first, then sender, tool and session, then orchestrator, then nvim.

## Why this split

In pi-mono, `pi-ai` handled both "what is a message" and "how do I talk to an LLM." That meant every time you changed the streaming logic, you risked breaking the message types that the whole system depended on. And `pi-agent-core` handled both "what is a tool" and "how does the agent loop work." Same problem.

The split follows a simple rule: if two things change for different reasons, they should be in different packages. Messages change when the data model evolves. The sender changes when you add a new LLM provider or fix streaming bugs. Tools change when you add new capabilities. The orchestrator changes when you change the agent's behavior — how it loops, when it stops, how it handles interrupts. The session format changes when what the file shows changes, and the frontend changes when the editor integration does.

## What's not here

This is a POC. There's no compaction (a session has a token ceiling, `:TASend` refuses above it, and you trim the file) and no permission system for tools. The `cmd/agent` entry point only runs inside Neovim (`--nvim`, see `entry-point.md`).

## System dependencies

Two external binaries must be on `$PATH`:
- `rg` (ripgrep) — used by the grep tool
- `fd` — used by the find tool

Both are standalone Rust binaries with zero runtime dependencies. The tests in `/integration` also need `nvim`.

## Build and run

```bash
mage build          # compile all packages
mage test           # run all tests
mage testverbose    # run tests with verbose output

go build -o bin/the-agent ./cmd/agent    # the binary the Neovim plugin starts
```

Then, in Neovim with the plugin installed (see `extras/lazy.lua`) and `KIMI_API_KEY` set or in the project's `.env`, `:TA <name>` opens a session, `:TASend` sends it and `:TAAbort` stops a turn.
