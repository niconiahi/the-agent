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

`BashReadTool(sandbox)` takes a `Sandbox{User, Home, Project}`. `Sandbox.Command` builds `sudo -n -u <User> /usr/bin/env <Environment()> /bin/bash -c <command>` with the project as its working directory, so the model can use pipes, redirects, semicolons — anything bash supports — while `_the-agent` can read the project but not write it: a write fails with "Permission denied", and the tool's description tells the model to use `bash_write` for that. The variables are set inside sudo because sudoers' `env_reset` drops the caller's: `HOME` is `_the-agent`'s home, which root owns, and `GOCACHE` and `GOMODCACHE` are the two cache folders in it, the only part `_the-agent` can write, and `TMPDIR` is `<project>/.the-agent/tmp`, which setup gives to `_the-agent` with an inheritable full-control ACL for me, so I can clear it without sudo; `PATH` is mine, so the same toolchain is found; `GIT_CONFIG_*` sets `safe.directory=*`, without which git refuses a repository owned by another user.

`-n` means sudo never prompts. When sudo itself refuses (stderr starting `sudo: ` with one of `setup.SUDO_REFUSALS`, exit 1, no stdout) the tool returns an error, not a result, naming `sudo the-agent setup <project>`. The context wraps the invocation context with a timeout; on cancel or timeout the tool sends `SIGTERM` to sudo, which relays it to the command — I can't `SIGKILL` a process another user owns — and `WaitDelay` (`BASH_STOP_GRACE`) stops waiting for its output after that.

Captures stdout and stderr separately, then combines them (stderr appended after stdout). Truncates at `MAX_OUTPUT_BYTES` (100KB, in `tool.go`, shared with grep and find) to prevent massive outputs from blowing up the conversation context.

On failure, it doesn't return an error — it returns the output with the exit code in the details. This is important. A command returning exit code 1 is not an exceptional failure — it's normal operation (think `grep` finding nothing). The model needs to see the output and the exit code to decide what to do next. Only truly exceptional failures (couldn't start sudo, sudo refused) return errors.

Its tests need a machine where setup has run on the repository; they skip with the setup command when `sudo -n -u _the-agent test -r <repo>` fails, except the one that checks an unknown user fails at once with the setup message.

`run_bash(ctx, sandbox, start, arguments)` is the part `bash_read` and `bash_write` share: argument parsing, timeout, output, truncation, exit code and the setup error. `start(ctx, script)` builds the command, so `bash_read` runs it in the project and `bash_write` in the clone. Running a command and collecting its stdout, stderr and exit code is `capture`, which `run_bash` and the clone's steps (`run_step`, where a non-zero exit is an error) both use.

## bash_write.go — BashWriteTool

For commands that must write the project, like `go mod tidy` or `go generate`. Same arguments, output and exit-code handling as `bash_read`, but the command runs in the project's persistent copy at `.the-agent/clone`, brought up to date first, and whatever it changed there is then applied to the project as ordinary agent edits. The mechanism is the same on macOS and Linux, which is where `cmd/agent` registers the tool.

`BashWriteTool(sandbox, clone, replay)` takes three things, so `tool` stays free of Neovim:

- `Clone{Project, Binary, Run}` (see `clone.go` below) is the copy: the project, the-agent's binary, whose hidden `sync` subcommand updates the copy, and the `Runner` every step goes through (`Sandbox.Command` in production).
- `Replay{Write, Delete}` applies one `Change{Path, Content, Mode, Link, Deleted}` to an absolute path: `Path` relative to the project, the new bytes and mode of a regular file, the target of a symbolic link, or a deletion. The `--nvim` binary passes `vimtool.Replay(client)`, the buffer-backed write and delete, so every replayed file is saved, undoable as one `u` and sidecar-protected (see `vimtool.md`).

Each call takes the project's lock, so two `bash_write` calls on one project run one after the other, even from two tool instances (a subagent's and its parent's). It then syncs and lists the clone, runs the command through `run_bash`, and, unless the turn was stopped, lists the clone again and applies the differences in path order. Files whose content and permissions already match the project are skipped (a command that only touched a file changes nothing). A new or changed file goes through `Replay.Write`, and its permissions are then set with `os.Chmod` when they differ, so a generated script stays executable. A deleted file goes through `Replay.Delete` if it's still there. A folder the command removed is removed from the project with `os.Remove` once the deleted files are gone, deepest first, and reported as `D <folder>/`; a folder that still holds something (a file I added while the command ran, say) is kept and reported as `<folder>/ (folder not empty)`. Deletions go first, then folders, then new and changed files, so a folder replaced by a file of the same name works; the report is in path order. Symbolic links and binary files (any NUL byte) are not applied, because a buffer can't hold them faithfully; neither is a path outside the project. The model's result is the command output followed by `Applied to the project:` with one `A`, `M` or `D` line per file, and `Not applied:` with the reason for each skipped file, or `No files changed.` The clone is left as the command left it; the next call's sync puts it back in line with the project, so whatever wasn't applied (a binary, a link, a build output) is gone by then.

Failures to sync or read the changes are tool errors; when sudo refused, the error names `sudo the-agent setup <project>`, like `bash_read`'s.

## clone.go — Clone

`Clone{Project, Binary, Run}` is `<project>/.the-agent/clone` (`layout.Clone(project)`), which setup creates owned by `_the-agent` with an inheritable full-control ACL for me. Every step runs as `_the-agent` through `Run`, with the project as the working directory and a `cd` into the clone in the script, and hands its results back on stdout, so my process never has to read the clone itself. The steps are:

1. `<binary> sync <project>`, then list the clone, in one call. The sync is the `clone` package (see `clone.md`): it copies only what changed since the last call and removes what the project no longer has. The listing is `MANIFEST_SCRIPT`: `find` without `.git` (at any depth) or `./.the-agent`, then inode, size, mtime with nanoseconds, type and mode, and path for each file and link (`stat -f '%i %z %Fm %p %N'` on macOS, `-printf '%i %s %T@ %y%m %p\n'` with GNU find on Linux), followed by `FOLDERS_SCRIPT`, the same `find` printing each folder's path.
2. Run the command.
3. List the clone again. A file only in the first listing is deleted, and so is a folder only in the first listing. A path only in the second, or whose signature changed, is new or changed, and those files come back in one tar stream (`ARCHIVE_SCRIPT`, `tar -c -f - --null -T -`, plus `--no-mac-metadata` on macOS) that Go reads with `archive/tar`.

The diff compares the clone with itself, before and after the command, instead of with the project. A file I save, or create, in the project while the command runs is then neither reverted nor deleted by the replay. `.git` is synced, so git works in the clone, but never listed, so a command that rewrites the index or `HEAD` changes nothing. A file name containing a newline can't be listed this way, so the call fails on it with an error. The path is stable, because Go mixes the absolute directory into its build-cache keys.

The lock is a mutex per project path in this process; two Neovim instances on one project are out of scope. The steps are plain shell run through `Run`, so `tool/bash_write_test.go` passes a runner that runs bash as me and the test binary itself as `Binary` (its `TestMain` answers `sync` with `clone.Run`), and covers the whole flow, except sudo, on macOS and Linux.

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
