# tool

This is what makes the agent a *coding* agent. Without tools, it's just a chatbot. With them, it can read your code, run your tests, edit your files, search your codebase.

The package has two layers: the tool shape (what a tool looks like) and the concrete implementations (what each tool does).

## tool.go — The shape

A `Tool` is a struct, not an interface. It has four fields:

- **Name** — what the model calls when it wants to use this tool (`"read"`, `"bash_read"`, etc.)
- **Description** — a natural language description that tells the model when and how to use the tool. This is part of the prompt, in a sense — the model reads these descriptions to decide which tool fits the task.
- **Parameters** — a `json.RawMessage` containing a JSON Schema. This tells the model what arguments the tool accepts, their types, and which are required. It's raw JSON because the schema is passed straight through to the LLM API — we never need to interpret it ourselves.
- **Execute** — a function that takes a context, a tool call ID, and the parsed arguments, and returns a `ToolResult` and an error.

Using a struct with a function field instead of an interface means tools are values, not types. You construct a tool by calling a factory function (`ReadTool()`, `BashReadTool(sandbox)`) that returns a configured `Tool`. No need to define a new type for each tool. The factory function wires up the parameters schema and the execute function, and that's it.

`ToolResult` is what a tool returns: content blocks (usually `TextContent` with the output) and optional details (tool-specific metadata like exit codes). The content blocks go back to the model as the tool's response. The details go to the UI or logging — the model doesn't see them.

`ToSchema` converts a `Tool` into a `sender.ToolSchema`. This is the bridge between "the tool as a runtime thing with an execute function" and "the tool as described to the LLM." The schema only has name, description, and parameters — no execute logic. The LLM just needs to know what to call and what arguments to pass. The orchestrator is the one that actually runs the function.

`NewTool` is a straightforward constructor. Nothing interesting — just sets the four fields.

## read.go — ReadTool

Reads file contents. Takes a `path` (required), optional `offset` (1-indexed line number to start from), and optional `limit` (max lines to read).

Uses `os.ReadFile` to slurp the whole file, splits into lines, then takes the requested slice. Output is formatted with line numbers (`1\tcontent`, `2\tcontent`, ...) — the tab-separated format that makes it easy for the model to reference specific lines.

Truncates at 5000 lines by default. If the file is longer than the limit, appends a note saying which lines are shown out of how many total. This prevents the model from flooding its own context with a huge file.

Returns an error if the file doesn't exist. This is intentional — the model should know it asked for something that isn't there, rather than getting an empty result and being confused.

The numbering and truncation live in `Numbered(content, line_range)`, which the buffer-backed read in `vimtool` shares; `LineRangeFrom(arguments)` turns the `offset` and `limit` arguments into a `LineRange`, defaulting to line 1 and `MAX_READ_LINES`. The `--nvim` binary uses that read and edit instead of these two (see `vimtool.md`).

## bash_read.go — BashReadTool

Runs shell commands as `_the-agent`, the unprivileged user `the-agent setup` creates (see `setup.md`). Takes a `command` (required) and optional `timeout` in seconds (default 120). There is no plain `bash` tool: a command that writes the project behind the buffers is exactly what the sandbox exists to stop.

`BashReadTool(sandbox)` takes a `Sandbox{User, Home, Project}`. `Sandbox.Command` builds `sudo -n -u <User> /usr/bin/env <Environment()> /bin/bash -c <command>` with the project as its working directory, so the model can use pipes, redirects, semicolons — anything bash supports — while `_the-agent` can read the project but not write it: a write fails with "Permission denied", and the tool's description tells the model to use `bash_write` for that. The variables are set inside sudo because sudoers' `env_reset` drops the caller's: `HOME`, `TMPDIR`, `GOCACHE` and `GOMODCACHE` point into `_the-agent`'s home, which it can write; `PATH` is mine, so the same toolchain is found; `GIT_CONFIG_*` sets `safe.directory=*`, without which git refuses a repository owned by another user.

`-n` means sudo never prompts. When sudo itself refuses (stderr starting `sudo: ` with one of `SUDO_REFUSALS`, exit 1, no stdout) the tool returns an error, not a result, naming `sudo the-agent setup <project>`. The context wraps the invocation context with a timeout; on cancel or timeout the tool sends `SIGTERM` to sudo, which relays it to the command — I can't `SIGKILL` a process another user owns — and `WaitDelay` (`BASH_STOP_GRACE`) stops waiting for its output after that.

Captures stdout and stderr separately, then combines them (stderr appended after stdout). Truncates at `MAX_OUTPUT_BYTES` (100KB, in `tool.go`, shared with grep and find) to prevent massive outputs from blowing up the conversation context.

On failure, it doesn't return an error — it returns the output with the exit code in the details. This is important. A command returning exit code 1 is not an exceptional failure — it's normal operation (think `grep` finding nothing). The model needs to see the output and the exit code to decide what to do next. Only truly exceptional failures (couldn't start sudo, sudo refused) return errors.

Its tests need a machine where setup has run on the repository; they skip with the setup command when `sudo -n -u _the-agent test -r <repo>` fails, except the one that checks an unknown user fails at once with the setup message.

`run_bash(ctx, sandbox, start, arguments)` is the part `bash_read` and `bash_write` share: argument parsing, timeout, output, truncation, exit code and the setup error. `start(ctx, script)` builds the command, so `bash_read` runs it in the project and `bash_write` in the clone.

## bash_write.go — BashWriteTool

For commands that must write the project, like `go mod tidy` or `go generate`. Same arguments, output and exit-code handling as `bash_read`, but the command runs in a copy-on-write clone of the project, and whatever it changed there is then applied to the project as ordinary agent edits.

`BashWriteTool(sandbox, cloner, replay)` takes three things, so `tool` stays free of Neovim and of any one way of cloning:

- `Cloner func(ctx) (Clone, error)` makes a fresh clone. A `Clone` has `Command(ctx, script)` (the `*exec.Cmd` that runs a script inside the clone), `Changes(ctx)` (what the command changed, as `[]Change`) and `Remove()`. `Change{Path, Content, Mode, Link, Deleted}`: `Path` relative to the project, the new bytes and mode of a regular file, the target of a symbolic link, or a deletion. macOS uses `Clonefile` below; Linux will plug in a bubblewrap overlay the same way.
- `Replay{Write, Delete}` applies one change to an absolute path. The `--nvim` binary passes `vimtool.Replay(client)`, the buffer-backed write and delete, so every replayed file is saved, undoable as one `u` and sidecar-protected (see `vimtool.md`).

Each call takes a lock, so two `bash_write` calls never share the clone, makes the clone, runs the command through `run_bash`, then, unless the turn was stopped, asks for the changes and applies them in path order. Files whose content and permissions already match the project are skipped (a command that only touched a file changes nothing). A new or changed file goes through `Replay.Write`, and its permissions are then set with `os.Chmod` when they differ, so a generated script stays executable. A deleted file goes through `Replay.Delete` if it's still there. Symbolic links and binary files (any NUL byte) are not applied, because a buffer can't hold them faithfully; neither is a path outside the project. The model's result is the command output followed by `Applied to the project:` with one `A`, `M` or `D` line per file, and `Not applied:` with the reason for each skipped file, or `No files changed.` The clone is removed when the call returns, whatever happened.

Failures to clone or read the changes are tool errors; when sudo refused, the error names `sudo the-agent setup <project>`, like `bash_read`'s.

## clonefile.go — Clonefile

The macOS `Cloner`. `Clonefile(project, clones, run)` clones the project to `ClonePath(project, clones)`, `<clones>/<project base name>-<first 12 hex digits of sha256(project path)>`: the same path every call, because Go mixes the absolute directory into its build-cache keys, and the hash keeps two projects with the same name apart. `cmd/agent` passes `~_the-agent/clones` and `sandbox.Command` as `run`.

Every step runs as `_the-agent` through `run`, with the project as the working directory and a `cd` into the clone in the script. This is how the clone stays readable: `_the-agent`'s home is mode 0700, so my process can't read the clone, and setup isn't asked to change that. Instead `_the-agent` does all the reading and hands the results over on stdout. The clone also has to be made by `_the-agent`, because a clone belongs to whoever creates it, and only an owner can let the command write it. The steps are:

1. Set aside any clone left at the path, by moving it into a fresh `<clones>/.removing.XXXXXX` and deleting that in the background (`clonefile(2)` needs a destination that doesn't exist, and a background delete of the path itself would race the next clone).
2. `cp -c -R <project> <clone>`, which clones each file with `clonefile(2)` and keeps nanosecond mtimes, then list the clone (`MANIFEST_SCRIPT`: `find` without any `.git`, then `stat -f '%i %z %Fm %p %N'` for each file and link, giving inode, size, mtime, mode and path).
3. Run the command (`Command`).
4. List the clone again. A path only in the first listing is deleted. A path only in the second, or whose inode, size, mtime or mode changed, is new or changed, and those files come back in one `tar -c --no-mac-metadata --null -T -` stream that Go reads with `archive/tar`.
5. `Remove` sets the clone aside as in step 1 and deletes it in the background.

The diff compares the clone with itself, before and after the command, instead of with the project. A file I save, or create, in the project while the command runs is then neither reverted nor deleted by the replay. `.git` is never listed, so a command that rewrites the index or `HEAD` changes nothing. A file name containing a newline can't be listed this way, so `Changes` fails on it with an error.

The steps are plain shell run through `run`, so the tests pass a runner that runs bash as me in a temporary directory and cover the whole flow, except sudo, on any Mac.

## edit.go — EditTool

Find-and-replace. Takes `path`, `old_text`, and `new_text`. Reads the file, finds the exact `old_text`, replaces it with `new_text`, writes the file back.

The key constraint: `old_text` must appear exactly once. If it's not found, error. If it appears multiple times, error. This forces the model to be precise about what it's editing — no accidental mass replacements. The model has to provide enough context in `old_text` to uniquely identify the location.

Returns a simple diff showing removed lines (prefixed with `-`) and added lines (prefixed with `+`). Not a proper unified diff — just enough for the model to confirm the edit was correct. The listing is `Diff(old_text, new_text)`, shared with `vimtool`'s edit.

## write.go — WriteTool

Creates or overwrites a file. Takes `path` and `content`. Creates parent directories if they don't exist (`os.MkdirAll`). Returns a confirmation with the file path.

Simple and intentionally blunt — this is a full overwrite, not an append. The model should use `edit` for surgical changes and `write` for creating new files or complete rewrites. The `--nvim` binary uses `vimtool`'s buffer-backed write instead (see `vimtool.md`).

## grep.go — GrepTool

Searches file contents by pattern. Shells out to `rg` (ripgrep) because reimplementing fast regex search across a codebase is a waste of time when one of the best tools ever made for this already exists.

Takes `pattern` (required), optional `path` (default `.`), optional `glob` filter, optional `ignore_case`, optional `limit` (default 100 matches).

Builds the `rg` command with `--no-heading --line-number --color=never` for clean, parseable output. Exit code 1 from ripgrep means "no matches found" — that's not an error, so it returns "No matches found" as text content.

Truncates output at 100KB. Same reasoning as bash_read — don't flood the context.

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
