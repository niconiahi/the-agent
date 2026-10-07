# tool

This is what makes the agent a *coding* agent. Without tools, it's just a chatbot. With them, it can read your code, run your tests, edit your files, search your codebase.

The package has two layers: the tool shape (what a tool looks like) and the concrete implementations (what each tool does).

## tool.go — The shape

A `Tool` is a struct, not an interface. It has four fields:

- **Name** — what the model calls when it wants to use this tool (`"read"`, `"bash"`, etc.)
- **Description** — a natural language description that tells the model when and how to use the tool. This is part of the prompt, in a sense — the model reads these descriptions to decide which tool fits the task.
- **Parameters** — a `json.RawMessage` containing a JSON Schema. This tells the model what arguments the tool accepts, their types, and which are required. It's raw JSON because the schema is passed straight through to the LLM API — we never need to interpret it ourselves.
- **Execute** — a function that takes a context, a tool call ID, and the parsed arguments, and returns a `ToolResult` and an error.

Using a struct with a function field instead of an interface means tools are values, not types. You construct a tool by calling a factory function (`ReadTool()`, `BashTool()`) that returns a configured `Tool`. No need to define a new type for each tool. The factory function wires up the parameters schema and the execute function, and that's it.

`ToolResult` is what a tool returns: content blocks (usually `TextContent` with the output) and optional details (tool-specific metadata like exit codes). The content blocks go back to the model as the tool's response. The details go to the UI or logging — the model doesn't see them.

`ToSchema` converts a `Tool` into a `sender.ToolSchema`. This is the bridge between "the tool as a runtime thing with an execute function" and "the tool as described to the LLM." The schema only has name, description, and parameters — no execute logic. The LLM just needs to know what to call and what arguments to pass. The orchestrator is the one that actually runs the function.

`NewTool` is a straightforward constructor. Nothing interesting — just sets the four fields.

## read.go — ReadTool

Reads file contents. Takes a `path` (required), optional `offset` (1-indexed line number to start from), and optional `limit` (max lines to read).

Uses `os.ReadFile` to slurp the whole file, splits into lines, then takes the requested slice. Output is formatted with line numbers (`1\tcontent`, `2\tcontent`, ...) — the tab-separated format that makes it easy for the model to reference specific lines.

Truncates at 5000 lines by default. If the file is longer than the limit, appends a note saying which lines are shown out of how many total. This prevents the model from flooding its own context with a huge file.

Returns an error if the file doesn't exist. This is intentional — the model should know it asked for something that isn't there, rather than getting an empty result and being confused.

## bash.go — BashTool

Executes shell commands. Takes a `command` (required) and optional `timeout` in seconds (default 120).

Runs via `exec.CommandContext` with `/bin/bash -c`, which means the model can use pipes, redirects, semicolons — anything bash supports. The context wraps the invocation context with a timeout, so both the agent's cancellation and the tool's timeout can kill the process.

Captures stdout and stderr separately, then combines them (stderr appended after stdout). Truncates at 100KB to prevent massive outputs from blowing up the conversation context.

On failure, it doesn't return an error — it returns the output with the exit code in the details. This is important. A command returning exit code 1 is not an exceptional failure — it's normal operation (think `grep` finding nothing). The model needs to see the output and the exit code to decide what to do next. Only truly exceptional failures (like "couldn't start bash") return errors.

## edit.go — EditTool

Find-and-replace. Takes `path`, `old_text`, and `new_text`. Reads the file, finds the exact `old_text`, replaces it with `new_text`, writes the file back.

The key constraint: `old_text` must appear exactly once. If it's not found, error. If it appears multiple times, error. This forces the model to be precise about what it's editing — no accidental mass replacements. The model has to provide enough context in `old_text` to uniquely identify the location.

Returns a simple diff showing removed lines (prefixed with `-`) and added lines (prefixed with `+`). Not a proper unified diff — just enough for the model to confirm the edit was correct.

## write.go — WriteTool

Creates or overwrites a file. Takes `path` and `content`. Creates parent directories if they don't exist (`os.MkdirAll`). Returns a confirmation with the file path.

Simple and intentionally blunt — this is a full overwrite, not an append. The model should use `edit` for surgical changes and `write` for creating new files or complete rewrites.

## grep.go — GrepTool

Searches file contents by pattern. Shells out to `rg` (ripgrep) because reimplementing fast regex search across a codebase is a waste of time when one of the best tools ever made for this already exists.

Takes `pattern` (required), optional `path` (default `.`), optional `glob` filter, optional `ignore_case`, optional `limit` (default 100 matches).

Builds the `rg` command with `--no-heading --line-number --color=never` for clean, parseable output. Exit code 1 from ripgrep means "no matches found" — that's not an error, so it returns "No matches found" as text content.

Truncates output at 100KB. Same reasoning as bash — don't flood the context.

In the `--nvim` binary grep is `vimtool.Grep`, which runs this tool unchanged and then fills Neovim's quickfix list (titled `the-agent grep`, a new list per call) with one entry per hit, so `]q` walks what the agent found. The model's result is byte for byte the rg output above; the hits are parsed back out of it, using the searched path as the filename when rg searched a single file and so printed only `line:text`.

## find.go — FindTool

Searches for files by glob pattern. Shells out to `fd` for the same reason grep shells out to `rg` — `fd` is fast, respects `.gitignore`, and produces clean output.

Takes `pattern` (required), optional `path` (default `.`), optional `limit` (default 1000 results).

Same exit code handling as grep — exit code 1 means no matches, not failure.

## ls.go — LsTool

Lists directory contents. Uses `os.ReadDir` (not a shell command) because this is simple enough that shelling out would be overhead.

Takes optional `path` (default `.`) and optional `limit` (default 500). Sorts alphabetically. Marks directories with a trailing `/` so the model can tell files from directories at a glance.

Truncates if there are more entries than the limit, with a note saying how many were shown out of how many total.

## Why all tools return []Content

Every tool returns a `ToolResult` with `[]message.Content`. Even though every tool currently returns a single `TextContent`, the slice is there because the model API expects tool results as content blocks. And some tools might eventually return images (think: a screenshot tool, or the read tool handling image files). Using a slice from the start means no refactoring needed when that happens.

## Why arguments are map[string]interface{}

The model sends tool arguments as JSON. The OpenAI API sends them as a JSON string that we parse into a map. Using `map[string]interface{}` instead of typed structs for each tool's arguments keeps things simple — each tool extracts what it needs via type assertions (`arguments["path"].(string)`). The JSON Schema in the tool's parameters already validates the shape on the model side. We could add validation on our side too, but for a POC, the type assertions are sufficient. They fail loudly if the model sends wrong types.

One gotcha: JSON numbers always come through as `float64` in Go's `encoding/json`. That's why offset, limit, and timeout are extracted as `float64` and then cast to `int`. This is a well-known Go JSON behavior, not a bug.
