# cmd/agent — The entry point

The binary has one interface: Neovim. The Lua plugin starts it with `jobstart({ bin, "--nvim" }, { rpc = true })`, so it lives exactly as long as that Neovim and inherits its working directory (the user's project). That directory, fixed when the job starts, is the one source of truth for where sessions and the system prompt live and where tools run: a later `:cd` in Neovim doesn't move them.

## main.go

Without `--nvim` the binary prints a short usage message pointing at the plugin and `:TA`, and exits with status 2. There is no terminal UI; the Bubble Tea `chat` package was deleted once the Neovim frontend worked.

With `setup` as the first argument, it runs `the-agent setup` (see `setup.md`) and exits: 0 when every step succeeded, 1 with the error on stderr otherwise.

With `--nvim`:

1. Load `.env` from the working directory (the user's project) into the environment.
2. Send `log` output to stderr. stdout is the msgpack-RPC pipe, so nothing else may write to it; the plugin surfaces stderr as a notification.
3. Build an `nvim.Config`: the `kimi-k2.5` model, the project (the working directory), the tools (read, bash_read, edit, write, filter, grep, find, ls, plus bash_write where `clone_for` has a cloner for the OS, macOS's `tool.Clonefile` for now, with `vimtool.Replay` applying its changes), the default system prompt (used only to seed `.the-agent/system_prompt.md` the first time `:TA` runs in a project, when `:TA` also seeds `.the-agent/.gitignore` with `/clone/` and `/tmp/` if there is none), the API key in `StreamOptions`, a `Ready` check and a `Sandbox` that calls `setup.Ready(project, binary)` with the binary's own path (`setup.Binary()`), which makes `:TA` refuse, naming `sudo the-agent setup`, in a project where `_the-agent` cannot read the project, run the binary, or write `.the-agent/clone` and `.the-agent/tmp`. A missing `KIMI_API_KEY` doesn't exit, because exiting would silently kill the RPC job; `:TASend` reports it instead.
4. `nvim.New(os.Stdin, os.Stdout, ...)`, `nvim.Attach(client, config)` to register the RPC handlers, then `Serve()` until Neovim exits.

Everything else (sessions, sending, streaming, abort, the ceiling) lives in the `nvim` and `session` packages; see `docs/VIM_NATIVE_DESIGN.md`.

## Installing

`extras/lazy.lua` is a lazy.nvim spec: its `build` runs `go build -o bin/the-agent ./cmd/agent`, which is where the plugin looks for the binary by default, and it loads lazily on `:TA`, `:TASend` and `:TAAbort` or on its `<leader>a` keys. On plain Neovim, put the repo on the runtimepath, build the same way, and call `require("the-agent").setup()`. Never build with a bare `go build` at the repo root: that overwrites the tracked `agent` binary.

### The blank import

The `_ "github.com/niconiahi/the-agent/sender"` import ensures the sender package's `init()` runs, which registers the Kimi provider. It's redundant with the regular `sender` import, but it makes the dependency explicit.
