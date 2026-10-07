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

## Milestone: Buffer-backed file tools

The agent's file tools go through Neovim buffers. `read` returns the buffer when it is loaded and clean and the disk otherwise, and records the buffer's `changedtick` per agent. `edit` replaces `old_text` in the buffer as one undo block and saves at once, loading files that aren't open into a buffer first; unmatched `old_text` changes nothing. After an edit, new diagnostics inside the edited region are appended to the result, waiting briefly for an attached LSP to publish. `grep` also fills the quickfix list. `write` sets a buffer's whole content, creating the file if needed, and `filter` runs a text-in, text-out command over a buffer like `:%!cmd`; both save at once as one undo block. An agent change to a buffer with my unsaved changes first saves my version to `<session>/unsaved/<path>.<timestamp>`, reloads the buffer from disk, applies the change and notifies me with the sidecar's path; one `u` then reverts only the agent's change, back to the disk version.

### Package: `vimtool`

**Constants**:
- `QUICKFIX_TITLE = "the-agent grep"` - title of the quickfix list grep fills
- `DIAGNOSTICS_WAIT = 500 * time.Millisecond` - longest an edit waits for an attached LSP to publish diagnostics

**Functions**:
- `Grep(client *neovim.Nvim) tool.Tool` - `tool.GrepTool` with the same result, plus one quickfix entry per hit
- `Read(client *neovim.Nvim) tool.Tool` - buffer-backed read that records `b:the_agent_ticks`
- `Edit(client *neovim.Nvim) tool.Tool` - buffer-backed edit, one undo block, saved, region tracked with an extmark, new diagnostics in the region appended to the result
- `Write(client *neovim.Nvim) tool.Tool` - buffer-backed write that creates the file if needed, one undo block, saved
- `Filter(client *neovim.Nvim) tool.Tool` - runs a shell command over a buffer like `:%!cmd`, one undo block, saved; a failing command changes nothing
- `WithSession(invocation_context context.Context, directory string, now func() time.Time) context.Context` - the calling agent's session directory and clock for the tools

### Package: `tool`

**Types**:
- `LineRange{Offset, Limit int}` - which lines a read returns

**Functions**:
- `LineRangeFrom(arguments map[string]any) LineRange` - a read's `offset` and `limit` arguments, defaulting to line 1 and `MAX_READ_LINES`
- `Numbered(content string, line_range LineRange) ToolResult` - read's numbered, truncated output
- `Diff(old_text string, new_text string) string` - edit's minus/plus listing (was `generate_diff`)

### Package: `nvim`

**Methods**:
- `run` puts the session directory and `Config.Now` on the turn's context with `vimtool.WithSession`

### Package: `nvim/nvimtest`

**Functions**:
- `StartWithTools(t *testing.T, config nvim.Config, build func(*neovim.Nvim) []tool.Tool) *Harness` - `Start` with tools bound to the harness's Neovim

### Lua plugin

- `lua/the-agent/buffer.lua` - `read`, `edit`, `write`, `filter` and `close_region`, each one RPC request; edit, write and filter share the sidecar-and-reload step for buffers with unsaved changes, which breaks the undo sequence after the reload; `edit` snapshots the diagnostics on the replaced lines and `close_region(region, timeout)` returns the new ones in the region

### Integration tests

- `integration/vimtool_helpers_test.go` - shared helpers for the vimtool tests: start Neovim with the vimtool tools and scripted tool calls, send a turn, read tool results, buffer lines, ticks, undo and recorded notifications
- `integration/fill_quickfix_from_grep_test.go` - a grep tool call fills the quickfix list with file, line and text per hit, for a directory and a single file, and the model's result is rg's output
- `integration/read_files_through_buffers_test.go`, `integration/edit_files_through_buffers_test.go` - scripted `read` and `edit` calls through `:TASend`; asserts the tool result, buffer, disk, undo and `changedtick`, and the sidecar, notification and one-undo-back-to-disk for a buffer with my unsaved changes
- `integration/write_files_through_buffers_test.go` - `write` creates a file, replaces an open buffer as one undo block, and sets my unsaved changes aside in a sidecar first, after which one undo reverts to the disk version
- `integration/filter_buffers_through_commands_test.go` - `filter` with `sort` and `gofmt` as one saved undo block; a failing command is a tool error that changes nothing
- `integration/report_edit_diagnostics_test.go` - diagnostics published through `vim.diagnostic.set` and through an in-process fake LSP; only new ones inside the edited region are reported, no LSP means no wait, and a silent LSP costs at most `DIAGNOSTICS_WAIT`

## Milestone: _the-agent sandbox

`sudo the-agent setup [project]` prepares a project so the agent's shell commands can run as `_the-agent`. On the first project it creates the user (no password, `/usr/bin/false` shell), writes `/etc/sudoers.d/the-agent` (`<me> ALL=(_the-agent) NOPASSWD: ALL`, installed only after `visudo -cf` accepts the draft) and creates `~_the-agent/{gocache,gomodcache,tmp,clones}`; on every project it grants an inheritable read ACL (`chmod +a` on macOS, `setfacl` on Linux), search on the parent folders up to my home, and records the project in `~_the-agent/projects`. Each step prints `exists` or `created`, so rerunning it changes nothing, and it ends with a check. It refuses `/`, my home or a folder containing it, and anything that isn't a directory. `--dry-run` prints every command without running one and needs no root, `--check` runs `sudo -n -u _the-agent ls <project>` and names Full Disk Access when macOS TCC blocks it, `--uninstall` undoes one project (keeping parent search other projects still need) and `--uninstall --all` removes every project's ACLs, the home, the sudoers file and the user.

### Package: `setup`

**Types**:
- `Host` - the machine setup acts on: system, invoking user and home, working directory, root or not, the shell and where output goes
- `Shell` - runs one `Command` and returns its combined output; the seam tests fake
- `Command{Args, Input}` - one command and its stdin; `String()` is what `--dry-run` prints

**Constants**:
- `USER = "_the-agent"`, `SUDOERS = "/etc/sudoers.d/the-agent"`, `SUDOERS_DRAFT` - the user and its sudoers file (the draft name has a `.`, so sudo ignores it)
- `DARWIN_HOME = "/var/the-agent"`, `LINUX_HOME = "/var/lib/the-agent"` - `~_the-agent`
- `DARWIN_READ`, `DARWIN_SEARCH` - the macOS ACL entries for a project and its parents
- `DARWIN_FIRST_ID = 400`, `DARWIN_LAST_ID = 499` - where the macOS user and group id is picked
- `USAGE` - the setup command line

**Functions**:
- `Run(host Host, arguments []string) error` - `the-agent setup` with its flags and optional project
- `Local(output io.Writer) (Host, error)` - this machine, with `SUDO_USER` as the invoking user when run under sudo

**Errors**:
- `ERROR_NOT_ROOT` - setup or uninstall without root and without `--dry-run`
- `ERROR_ALL_WITHOUT_UNINSTALL` - `--all` alone
- `ERROR_UNSUPPORTED_SYSTEM` - neither macOS nor Linux
- `ERROR_NO_FREE_ID` - no free macOS id between 400 and 499

### Package: `cmd/agent`

- `the-agent setup …` runs `setup.Run` on `setup.Local`, printing to stdout and exiting 1 with the error on stderr

### Integration tests

- `integration/set_up_the_agent_sandbox_test.go` - the built binary's `setup --dry-run` prints the project's ACL step and check without root, and `setup /` is refused with exit status 1

### `bash_read` replaces `bash`

The `bash` tool is gone. `bash_read` runs every command as `_the-agent` in the real project, through `sudo -n -u _the-agent /usr/bin/env HOME=… PATH=… TMPDIR=… GOCACHE=… GOMODCACHE=… GIT_CONFIG_*=… /bin/bash -c <command>`, so tests, builds and `git log` work as before while any write to the project fails with "Permission denied" (the description tells the model to use `bash_write` then). `TMPDIR` and the Go caches are `_the-agent`'s own under `~_the-agent`, set inside the sudo'd command because sudoers' `env_reset` drops whatever the caller passes; `PATH` is mine, so the same `go` is found; `safe.directory=*` keeps git from refusing a repository another user owns. `sudo -n` never prompts: when sudo itself refuses (unknown user, password required, not allowed) the tool returns an error naming `sudo the-agent setup <project>` instead of a result. A timeout or abort sends `SIGTERM`, which sudo relays to the command (it can't be `SIGKILL`ed across users), and gives up after `BASH_STOP_GRACE`. `:TA` in a project `_the-agent` can't read says so, tells me to run `sudo the-agent setup`, and creates nothing, not even `.the-agent/`.

### Package: `tool`

- `BashReadTool(sandbox Sandbox) Tool` - `bash_read`, replacing `BashTool`
- `Sandbox{User, Home, Project}` - who runs commands, its home (caches and `TMPDIR` live there) and the project; `Environment()` is the variables set inside sudo, `Command(ctx, directory, script)` the `*exec.Cmd` (for `bash_write` too)
- `MAX_OUTPUT_BYTES` moved to `tool.go`; `DEFAULT_BASH_TIMEOUT = 120s`, `BASH_STOP_GRACE = 5s`, `SUDO_REFUSALS` - sudo's own refusals, reported as missing setup

### Package: `setup`

- `Home() string` - `~_the-agent` on this system
- `Ready(project string) error` - the `--check` probe (`sudo -n -u _the-agent ls <project>`) and its diagnosis, for `:TA`

### Package: `nvim`

- `Config.Sandbox func(project string) error` - checked by `:TA` before it creates anything; nil skips it

### Package: `nvim/nvimtest`

- `LaunchIn(t, directory)` - Neovim in a given project; `StartWithTools` uses it when `config.Project` is set

### Package: `cmd/agent`

- the `--nvim` binary registers `bash_read` with the working directory as the project and sets `Sandbox: setup.Ready`

### Tests

- `tool/bash_read_test.go` - an unknown sandbox user fails at once with the setup command; the old `bash` tests (exit code, stderr, timeout, cancel, truncation) run through `bash_read` and skip when `sudo -n -u _the-agent test -r <repo>` fails
- `cmd/agent/main_test.go` - the tool list has `bash_read` and no `bash`
- `integration/open_session_test.go` - `:TA` in a project that isn't set up names `sudo the-agent setup` and leaves no `.the-agent/`
- `integration/run_binary_as_neovim_job_test.go`, `integration/install_with_lazy_nvim_test.go` - the built binary's `:TA` in a `t.TempDir()` project (mode 0700, never readable by `_the-agent`) now answers with the setup message; session creation by the binary and the `:cd` check moved to `TestNvimMode_OpensSessionsInASetUpProject`, which skips unless setup has run on the repository
- `integration/run_commands_as_the_agent_test.go` - in a temp project inside the repo (`$TMPDIR` is mode 0700, so `_the-agent` can't enter it): a write is "Permission denied" and changes nothing, `$TMPDIR` is `~_the-agent/tmp` and writable, `go test` passes with `~_the-agent`'s caches; all skip with the setup command when `_the-agent` can't read the project

### `bash_write` on macOS

`bash_write` runs a command that must write the project, such as `go mod tidy` or `go generate`, as `_the-agent` in a copy of the project. Every file the command created, changed or deleted, outside `.git`, then comes back as an agent edit through the buffers: saved, undone with one `u`, and with my unsaved changes set aside in a sidecar first. A deleted file is removed from disk and its buffer wiped. The copy is diffed against its own listing from before the command, so files I save in the project meanwhile are left alone. Symbolic links and binary files are reported, not applied. Every step that reads the copy runs as `_the-agent` through the same `sudo -n` rule. (Where the copy lives and how it is kept up to date is "`bash_write` syncs a persistent clone on every Unix" below; the bubblewrap overlay once planned for Linux was dropped, because overlayfs checks writes against the original files' owner.)

### Package: `tool`

- `BashWriteTool` - `bash_write`; its result is the command output plus `Applied to the project:` (`A`/`M`/`D` lines), `Not applied:` or `No files changed.`
- `Change{Path, Content, Mode, Link, Deleted}` - one change the command made
- `Replay{Write, Delete}` - how a change reaches the project, injected so `tool` stays free of Neovim
- `run_bash` takes the command builder instead of a directory, so both bash tools share it

### Package: `vimtool`

- `Replay(client) tool.Replay` - `Write` is the buffer-backed `write`; `Delete` removes the file and wipes its buffer, after a sidecar for unsaved changes

### Lua plugin

- `require("the-agent.buffer").delete(path, agent, stamp)` - sidecar for unsaved changes, `os.remove`, `nvim_buf_delete({force = true})`

### Package: `cmd/agent`

- `bash_write` is registered, and the seeded system prompt mentions it

### Tests

- `tool/bash_write_test.go` - with a runner that runs bash as me, so no sudo is involved: changed, new and deleted files are replayed, `.git` and same-content rewrites are ignored, links and binaries are reported, an executable keeps its mode, and project changes made during the command survive; an unknown sandbox user fails at once with the setup command
- `integration/apply_shell_writes_as_edits_test.go` - through Neovim with the local runner: a change shows up in the open buffer, saved, and one `u` reverts it; new files are created and deleted ones removed with their buffer wiped; unsaved changes to a changed or a deleted file go to sidecars first
- `integration/run_commands_as_the_agent_test.go` - `bash_write` as the real `_the-agent`: the edit is applied and undoable, and both calls run in the same clone; skips when setup is missing

### Setup prepares `.the-agent/clone`

`the-agent setup` now prepares each project's copy for `bash_write` inside the project. On every project it creates `.the-agent/` if missing (owned by me), and `.the-agent/clone` and `.the-agent/tmp` owned by `_the-agent`, mode 0700, with an inheritable full-control ACL for me: `chmod +a '<me> allow list,add_file,search,delete,add_subdirectory,delete_child,…,file_inherit,directory_inherit'` on macOS, `setfacl -m u:<me>:rwx,d:u:<me>:rwx,m::rwx,d:m::rwx` on Linux. So I can delete anything `_the-agent` creates there without sudo. It also grants `_the-agent` search on the folders holding the-agent's binary (skipping folders anyone can already search, such as `/usr/local/bin`, and those the project step covers), so `_the-agent` can run it. `~_the-agent` keeps only the Go caches: `tmp` and `clones` are no longer created there. The check, run by `--check` and by `:TA`, now also probes that `_the-agent` can run the binary and write `.the-agent/clone` and `.the-agent/tmp`, and names `sudo the-agent setup <project>` when it can't. `--uninstall` removes the project's clone and tmp; `--uninstall` of one project keeps search on the binary's folders, `--uninstall --all` removes it. `bash_read`'s `TMPDIR` is now `<project>/.the-agent/tmp`, and `:TA` seeds `.the-agent/.gitignore` with `/clone/` and `/tmp/` when there is none.

On Linux the inherited ACL covers what `_the-agent` creates natively; a copy (`cp`, `rsync`) requests the source's mode, which caps the ACL mask, so copied files are not deletable by me until something restores the mask. The sync below does that.

### Package: `setup`

- `Host.Binary` - the-agent's binary, symlinks resolved; `Local` fills it in
- `Binary() (string, error)` - the running executable, symlinks resolved
- `Ready(project, binary string) error` - the `--check` probes run on this machine: read the project, run the binary, write `.the-agent/clone` and `.the-agent/tmp`
- `CACHES` is now `gocache`, `gomodcache`; `FOLDER = ".the-agent"`, `OWNED = clone, tmp`

### Package: `nvim`

- `:TA` seeds `.the-agent/.gitignore` (`GITIGNORE`) when it doesn't exist and never overwrites one

### Package: `tool`

- `Sandbox.Environment()` sets `TMPDIR=<project>/.the-agent/tmp`

### Package: `cmd/agent`

- the `--nvim` binary passes its own path to `setup.Ready`

### Tests

- `setup/setup_test.go` - the first-project, later-project and Linux dry runs print the clone, tmp, ACL and binary-search commands; a set-up project prints `exists` for every step; `--check` names setup when `_the-agent` can't run the binary or write the clone; `--uninstall` removes the clone and tmp and reports `absent` when they're gone
- `tool/bash_read_test.go` - `TMPDIR` is the project's `.the-agent/tmp`
- `integration/open_session_test.go` - `:TA` seeds `.the-agent/.gitignore` once and keeps an existing one
- `integration/run_commands_as_the_agent_test.go` - the project is now the set-up repository itself (only setup, as root, can make its `.the-agent/{clone,tmp}`), with each test working in a temp folder inside it and its own session; I can delete files and folders `_the-agent` created in `.the-agent/clone` and `.the-agent/tmp`; `$TMPDIR` is `<project>/.the-agent/tmp`; all skip unless `_the-agent` can write the project's clone and tmp
- `sender/kimi_integration_test.go` - `defer cancel()` for `go vet`

### `bash_write` syncs a persistent clone on every Unix

`bash_write` now works the same way on macOS and Linux, and is registered on both. Its copy is the project's `.the-agent/clone`, kept between calls. Each call takes a lock for the project, so two `bash_write` calls on one project run one after the other, then runs the hidden `the-agent sync <project>` as `_the-agent`, lists the clone, runs the command there, lists it again and replays the differences as before. The sync skips `.the-agent/`, keeps `.git`, copies only the files whose size, nanosecond mtime or mode differ from the clone's, file by file (`clonefile(2)` on macOS, `FICLONE` on Linux where btrfs or XFS has it, a plain copy otherwise), copies symbolic links as links, and removes whatever the project no longer has, so whatever a previous command left in the clone is gone. Each step leaves a state the next sync can finish from, so a sync that is interrupted or fails is completed by the next one. Every clone folder keeps its owner's `rwx` (a command's `chmod 0555` can't block the next sync) and, on Linux, its group bits, which are the ACL mask, at `rwx`, so I can delete the clone without sudo. This replaces the fresh `cp -c -R` clone per call in `~_the-agent/clones` and its background delete.

### Package: `clone`

- `Sync(project, clone string) (Report, error)` - bring the clone up to date; `Report{Copied, Cloned, Removed}` lists what was copied, what of that was copy-on-write, and what was removed
- `Run(arguments []string) error` - the `the-agent sync <project>` subcommand; nothing on stdout
- `Path(project string) string` - `<project>/.the-agent/clone`
- `FOLDER = ".the-agent"`; `DIRECTORY_BITS` - 0700, or 0770 on Linux, where the group bits are the ACL mask

### Package: `tool`

- `BashWriteTool(sandbox Sandbox, clone Clone, replay Replay) Tool` - takes the clone instead of a `Cloner`; serialized per project
- `Clone{Project, Binary, Run}`, `Clone.Path()` - the in-project clone, synced by `Binary`'s `sync` subcommand, every step run through `Run`
- `MANIFEST_SCRIPT` (BSD `stat` on macOS, GNU `find -printf` on Linux, both skipping `.git` and `./.the-agent`), `ARCHIVE_SCRIPT` - the listing and the tar stream of changed files
- removed: `Cloner`, the `Clone` interface and `Remove()`, `Clonefile`, `ClonePath`

### Package: `cmd/agent`

- `the-agent sync <project>` - hidden subcommand, runs `clone.Run`
- `bash_write` is registered on macOS and Linux; `default_tools` takes the binary's path; `clone_for` is gone

### Tests

- `clone/sync_test.go` - as me, no sudo: the first sync copies the project with `.git` and without `.the-agent`; a second copies only files changed since the first; what a command did in the clone is undone, including in a read-only folder; links stay links; a sync stopped halfway is completed by the next; files are cloned on APFS and btrfs and copied on ext4, tmpfs and overlayfs
- `tool/bash_write_test.go` - no longer macOS-only; the test binary serves `sync` from `TestMain`; the command runs in `.the-agent/clone`, which is kept between calls; every call starts from the project as it is now; two calls on one project don't overlap
- `cmd/agent/main_test.go` - `bash_write` is among the tools on macOS and Linux
- `integration/apply_shell_writes_as_edits_test.go` - no longer macOS-only; uses the built binary's `sync`
- `integration/run_commands_as_the_agent_test.go` - `bash_write` as the real `_the-agent` on any Unix, run in `<project>/.the-agent/clone`; I can delete what the sync copied without sudo; skips when setup is missing

### Sandbox review fixes

`_the-agent` no longer owns its home. Setup gives `~_the-agent` to root, mode 0755, and gives `_the-agent` only `gocache` and `gomodcache` inside it, never recursively, so `_the-agent` writes only its two caches and the project's `.the-agent/clone` and `.the-agent/tmp`. Before, it owned the home and so could replace root's registry `~_the-agent/projects`, and `--uninstall --all` would then `rm -rf` and strip ACLs on whatever paths it listed. A home set up by an earlier version is taken back on the next setup: caches that aren't folders are removed and recreated, and a registry that is a link or isn't root's is rewritten by root. Setup, `--uninstall` and `--uninstall --all` also refuse a project whose folder, `.the-agent`, `clone` or `tmp` is a symbolic link or isn't a folder, before changing anything. The sudoers step now checks that `/etc/sudoers.d/the-agent` holds my line, not just that it exists, and adds it to the lines already there (still through the draft, `visudo -cf` and `mv`).

`bash_write` now removes from the project the folders the command removed in the clone, once the deleted files are gone, and reports them as `D <folder>/`; a folder that still holds something of mine is kept and reported as not applied.

### Package: `layout`

- new, imports nothing: `FOLDER`, `CLONE`, `TMP`, `SESSIONS`, `SYSTEM_PROMPT`, `GITIGNORE`, `GO_CACHE`, `GO_MODULE_CACHE`, `OWNED`, `CACHES`; `Folder(project)`, `Clone(project)`, `Tmp(project)` - the one source of the `.the-agent` layout and the cache names

### Package: `setup`

- `Command.Args` is now `Command.Arguments`
- `Quote(word string) string` - the shell quoting `Command.String()` uses, exported for `tool`
- `SUDO_REFUSALS`, `Refused(output string) bool` - sudo refusing to run a command, shared with `tool`
- `Supported(system string) bool` - whether setup knows the system
- removed: `CACHES`, `FOLDER`, `OWNED` (now in `layout`)

### Package: `clone`

- removed: `Path` and `FOLDER` (now `layout.Clone` and `layout.FOLDER`)

### Package: `tool`

- `Change.Folder` - a folder the command removed
- `FOLDERS_SCRIPT` - lists the clone's folders after `MANIFEST_SCRIPT`
- removed: `Clone.Path()`, `SUDO_REFUSALS` (now `setup.SUDO_REFUSALS`)

### Package: `cmd/agent`

- `bash_write` is registered where `setup.Supported(runtime.GOOS)`

### Tests

- `setup/setup_test.go` - the dry runs give the home to root and only the caches to `_the-agent`; a home `_the-agent` owned, with a linked cache and a linked registry, is taken back; `--uninstall` refuses a linked `.the-agent` and `--uninstall --all` a clone that isn't a folder, running nothing that changes the machine; a sudoers file without my line gets it added to the existing one; an unreadable sudoers file counts as present in a dry run without root
- `tool/bash_write_test.go` - folders the command removed, nested or empty, are removed from the project; one still holding my file is kept and reported

## Milestone: Streaming edit previews

I watch an `edit` happen while its arguments stream. A dedicated follow window, opened beside everything else and never the window I am in, shows the file as soon as `path` has streamed; once `old_text` has streamed the region is found, scrolled to and highlighted; while `new_text` streams it grows as virtual lines under the region. The preview is only extmarks, so the buffer text never changes until the real `edit` runs (one undo block, saved), after which the extmarks are gone; an aborted turn, a stream that fails mid-call or an edit that fails clears them and leaves the file byte-identical. Updates go to Neovim at most once per `FLUSH_INTERVAL`.

### Package: `partialjson`

**Types**:
- `Fields{Complete map[string]string, Streaming string, Partial string}` - the string fields complete so far and the decoded prefix of the one streaming

**Functions**:
- `Read(text string) Fields` - reads the accumulated fragments of a streaming JSON object; splits mid-key, mid-escape, mid-surrogate-pair and mid-UTF-8 never show half a character

### Package: `sender`

- `EventToolCallStart` carries the call's `ID` and `Name`

### Package: `nvim`

**Constants**:
- `PREVIEW_TOOL = "edit"` - the tool whose streaming arguments are previewed

### Package: `nvim/nvimtest`

**Types**:
- `Gate` (`NewGate`, `Step`) - holds a scripted stream before each tool-argument fragment and before the `ToolCallEnd`
- `Reply.Fragments`, `Reply.Gate` - stream a tool call's arguments in given fragments, paused by a gate

### Lua plugin

- `lua/the-agent/follow.lua` - the follow window (`w:the_agent_follow`) and `preview(id, fields)` / `clear(id)` on namespace `the-agent-preview`, highlights `TheAgentPreviewOld` and `TheAgentPreviewNew`
- `lua/the-agent/buffer.lua` - `open(path)` loads a file without showing it; `locate(buffer, old_text)` is the region `edit` replaces

### Tests

- `partialjson/partialjson_test.go` - table tests for splits mid-key, mid-escape, mid-unicode-escape, mid-surrogate-pair and mid-UTF-8, skipped non-string values, and every byte prefix of an escape-heavy document agreeing with `encoding/json`
- `sender/kimi_tool_call_start_test.go` - the start event names the call before its arguments stream
- `integration/preview_streaming_edits_test.go` - the fake stream pauses between fragments to assert the follow window, highlight, virtual text, untouched buffer and unmoved current window at each stage; then the real edit with no extmarks left and one undo; abort, a failing stream and a failing edit clear the preview and leave the file byte-identical
