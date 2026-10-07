# vimtool

The file tools of the `--nvim` binary, backed by Neovim buffers instead of `os.WriteFile`. When the agent changes a file, the change lands in the buffer I may be looking at, one `u` takes it back, the LSP sees it as an ordinary buffer change, and it is saved at once so `bash`, `grep` and builds read it from disk too. `find`, `ls` and `bash` stay in `tool`; `Grep` here wraps `tool`'s grep and fills the quickfix list with its hits (see `tool.md`).

Each tool is built with the Neovim client it talks to (`Read(client)`, `Edit(client)`, `Tools(client)` for all of them), so `cmd/agent` builds them after it connects and tests bind them to the harness with `nvimtest.StartWithTools(t, config, vimtool.Tools)`. The logic runs in Lua, `lua/the-agent/buffer.lua`, one function per tool step, so each step is a single RPC request. Everything one request changes is one undo block, which is what makes an agent edit exactly one `u`.

Paths may be absolute or relative; relative paths resolve against Neovim's working directory, the project.

## Which agent is calling

`nvim` puts the session directory, `.the-agent/sessions/<name>`, and its clock (`nvim.Config.Now`) on the turn's context with `WithSession` before it runs the agent, and the tools read them back. The session directory is the agent's identity (one agent per session for now), and it is where per-session files such as unsaved-change sidecars belong; the clock stamps those sidecars, so tests can fix it.

## My unsaved changes

`edit`, `write` and `filter` all get their buffer ready the same way: the loaded buffer, or the file loaded into a listed, hidden buffer. When that buffer has unsaved changes of mine, my version is first written, byte for byte as `:write` would, to `<session>/unsaved/<path>.<timestamp>`, where `<path>` is the file's path relative to the project (kept as subdirectories, so `src/b.txt` goes to `unsaved/src/b.txt.<timestamp>`) and `<timestamp>` is UTC RFC 3339, e.g. `2026-10-06T14:32:00Z`. The buffer is then reloaded from disk with `:edit!`, the agent's change is applied to that and saved, and a warning notification names the sidecar's path. No file is ever half mine and half the agent's, and nothing of mine is lost. A clean buffer only gets a `checktime`, so changes made on disk underneath are picked up first.

## read.go — Read

Returns the buffer when the file is loaded and has no unsaved changes of mine, after a `checktime` so a file changed on disk underneath is reloaded first. When the buffer has my unsaved changes, or the file isn't loaded, it returns the file on disk, so the model never reasons over my half-finished work. Loading the file is not needed for a read, so a read does not open buffers.

When the file is loaded, `read` records the buffer's `changedtick` for the calling agent in the buffer variable `b:the_agent_ticks`, a dictionary from session directory to tick. Nothing checks it yet; it is there for the cross-agent staleness check.

The output is the same numbered-lines format as `tool`'s read (`tool.Numbered`), with the same `offset`, `limit` and truncation.

## edit.go — Edit

Finds `old_text` in the buffer, which must occur exactly once, and replaces it with `nvim_buf_set_text`, then writes the buffer, all in one request. A file that isn't open is loaded into a listed, hidden buffer first, so it is edited, saved and undoable the same way. Unmatched or repeated `old_text` returns a tool error and changes nothing.

A buffer with my unsaved changes goes through the sidecar flow above first, so the agent never saves my work for me and `old_text` is matched against the disk version.

The edited text is tracked with an extmark in the `the-agent-edit` namespace, never with stored line numbers. `edit` returns that region to Go and releases it once the tool call is done; that is the place to look at the region after the edit, for example for diagnostics. The result is `Edited <path>` and the same minus/plus listing as `tool`'s edit (`tool.Diff`).

## write.go — Write

Sets a buffer's whole content and saves it, creating the file and its parent directories when they don't exist yet. Content without a final newline is saved without one. The change is one undo block and is tracked with an extmark over the whole buffer like an edit's. The result is `Wrote <path>`. Its description steers the model to `edit` for existing files, so changes stay small and reviewable.

## filter.go — Filter

Runs a text-in, text-out shell command (`sort`, `gofmt`, a `sed` expression) over the whole buffer through `nvim_buf_call`, like `:%!command`, then saves, so it is one undo block like any agent edit. `%`, `#` and `!` in the command are escaped, so the command reaches the shell as written. A command that exits non-zero would leave its output in the buffer, so it is undone in the same request: the buffer, the disk and the undo history are as before, and the tool error carries the exit code and output. The result is `Filtered <path> through <command>`.
