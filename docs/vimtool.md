# vimtool

The file tools of the `--nvim` binary, backed by Neovim buffers instead of `os.WriteFile`. When the agent changes a file, the change lands in the buffer I may be looking at, one `u` takes it back, the LSP sees it as an ordinary buffer change, and it is saved at once so `bash_read`, `grep` and builds read it from disk too. `find`, `ls` and `bash_read` stay in `tool`, and so does `bash_write`, which gets this package's write and delete injected (`Replay` below); `Grep` here wraps `tool`'s grep and fills the quickfix list with its hits (see `tool.md`).

Each tool is built with the Neovim client it talks to (`Read(client)`, `Edit(client)`, `Write(client)`, `Filter(client)`, `Grep(client)`), so `cmd/agent` builds them after it connects and tests bind them to the harness with `nvimtest.StartWithTools`. The logic runs in Lua, `lua/the-agent/buffer.lua`, one function per tool step, so each step is a single RPC request (`call_buffer_function` in `vimtool.go`). Everything one request changes is one undo block, which is what makes an agent edit exactly one `u`.

`edit`, `write` and `filter` share one Go step, `change_buffer`: it calls the Lua function that changes and saves the buffer, then `close_region` with how long to wait for diagnostics (`DIAGNOSTICS_WAIT` for `edit`, none for the other two), and returns the diagnostics found. Required string arguments such as `path` are checked by `required_string`, which returns `<name> is required` as the tool error.

Paths may be absolute or relative; relative paths resolve against Neovim's working directory, the project.

## Which agent is calling

`nvim` puts the session directory, `.the-agent/sessions/<name>`, and its clock (`nvim.Config.Now`) on the turn's context with `WithSession` before it runs the agent, and the tools read them back. The session directory is the agent's identity (one agent per session for now), and it is where per-session files such as unsaved-change sidecars belong; the clock stamps those sidecars, so tests can fix it.

## My unsaved changes

`edit`, `write` and `filter` all get their buffer ready the same way: the loaded buffer, or the file loaded into a listed, hidden buffer. When that buffer has unsaved changes of mine, my version is first written, byte for byte as `:write` would, to `<session>/unsaved/<path>.<timestamp>`, where `<path>` is the file's path relative to the project (kept as subdirectories, so `src/b.txt` goes to `unsaved/src/b.txt.<timestamp>`) and `<timestamp>` is UTC RFC 3339, e.g. `2026-10-06T14:32:00Z`. The buffer is then reloaded from disk with `:edit!`, the agent's change is applied to that and saved, and a warning notification names the sidecar's path. The reload and the agent's change happen in one request, which would make them one undo block, so the undo sequence is broken right after the reload (`:h undo-break`, by setting `undolevels` to itself): one `u` reverts exactly the agent's change and leaves the disk version as it was before, and my unsaved version lives in the sidecar (a second `u` also brings it back, through `undoreload`). No file is ever half mine and half the agent's, and nothing of mine is lost. A clean buffer only gets a `checktime`, so changes made on disk underneath are picked up first.

## read.go — Read

Returns the buffer when the file is loaded and has no unsaved changes of mine, after a `checktime` so a file changed on disk underneath is reloaded first. When the buffer has my unsaved changes, or the file isn't loaded, it returns the file on disk, so the model never reasons over my half-finished work. Loading the file is not needed for a read, so a read does not open buffers.

When the file is loaded, `read` records the buffer's `changedtick` for the calling agent in the buffer variable `b:the_agent_ticks`, a dictionary from session directory to tick, which `edit`'s staleness check compares against (below). A file that isn't loaded gets no tick on purpose: a read does not load buffers, and a buffer that doesn't exist has no `changedtick` to record. So the baseline the staleness check sees is this: an agent has a tick for a buffer only once it read it while it was loaded, or changed it with `edit`, `write` or `filter` (which always load the file and record the tick after saving); a buffer an agent has no tick for counts as never seen by that agent.

The output is the same numbered-lines format as `tool`'s read (`tool.Numbered` with `tool.LineRangeFrom(arguments)`), with the same `offset`, `limit` and truncation.

## edit.go — Edit

Finds `old_text` in the buffer, which must occur exactly once, and replaces it with `nvim_buf_set_text`, then writes the buffer, all in one request. A file that isn't open is loaded into a listed, hidden buffer first, so it is edited, saved and undoable the same way. Unmatched or repeated `old_text` returns a tool error and changes nothing.

An agent never edits from a stale view. Before anything else, when the file is loaded and the calling agent has a tick for it in `b:the_agent_ticks`, `edit` compares that tick with the buffer's current `changedtick`; a clean buffer gets a `checktime` first, so a change made on disk underneath counts too. If the count moved, someone else changed the buffer since the agent last saw it, me typing or another agent's edit, and `edit` returns the tool error `file changed since you read it, re-read first` without touching the buffer, the disk or a sidecar. The agent's own changes don't make its view stale, because `edit`, `write` and `filter` record the tick after saving, so consecutive edits after one `read` go through, and a fresh `read` records the new tick and clears the rejection. A buffer the agent has no tick for (it never read it while loaded, nor changed it) is not checked. The tick lives in the buffer; the check is self-contained in `buffer.lua` and shares no state with the write leases and locks of the other concurrency mechanisms.

A buffer with my unsaved changes goes through the sidecar flow above first, so the agent never saves my work for me and `old_text` is matched against the disk version.

The edited text is tracked with an extmark in the `the-agent-edit` namespace, never with stored line numbers. `edit` returns that region to Go, which closes it with `close_region` once the change is saved. The result is `Edited <path>` and the same minus/plus listing as `tool`'s edit (`tool.Diff`).

## diagnostics.go — what the edit broke

The model learns what it broke without running anything. Before replacing the text, `edit` counts the diagnostics already on the replaced lines and starts listening for `DiagnosticChanged` on the buffer. Closing the region then waits, up to `DIAGNOSTICS_WAIT` (500 ms), for that event, and only when an LSP client is attached to the buffer: with no LSP nothing will publish, so the edit returns at once. Diagnostics that arrive during the save itself (a `BufWritePost` checker) are picked up without waiting. The wait ends at the first `DiagnosticChanged`, so a server that publishes in several rounds may report only some of them; completeness is not promised. The wait is also not tied to a document version: Neovim's `publishDiagnostics` handler drops the version the server sends and `DiagnosticChanged` carries none, so a late publish for the text before the edit, arriving after the edit, ends the wait too and may report what was already there or miss what the edit broke. Telling those apart would mean wrapping the LSP client's handler, which is more than this best-effort report is worth.

Whatever diagnostics then sit inside the region, minus the ones counted before (matched by namespace, severity and message, since Neovim does not move stored diagnostic positions with buffer edits), are appended to the result:

```
Diagnostics in the edited region:
3:5 error: undefined: foo (gopls)
```

Lines and columns are 1-based; `diagnostics_section` formats the section. Diagnostics outside the region are never reported, and with none to report the section is left out. `write` and `filter` close their regions without a snapshot or a wait, so they report no diagnostics.

## write.go — Write

Sets a buffer's whole content and saves it, creating the file and its parent directories when they don't exist yet. Content without a final newline is saved without one. The change is one undo block and is tracked with an extmark over the whole buffer like an edit's. The result is `Wrote <path>`. Its description steers the model to `edit` for existing files, so changes stay small and reviewable.

## filter.go — Filter

Runs a text-in, text-out shell command (`sort`, `gofmt`, a `sed` expression) over the whole buffer through `nvim_buf_call`, like `:%!command`, then saves, so it is one undo block like any agent edit. `%`, `#` and `!` in the command are escaped, so the command reaches the shell as written. A command that exits non-zero would leave its output in the buffer, so it is undone in the same request: the buffer, the disk and the undo history are as before, and the tool error carries the exit code and output. The result is `Filtered <path> through <command>`.

## replay.go — Replay

`Replay(client)` returns the `tool.Replay` that `bash_write` applies its changes with, so `tool` never imports Neovim. `Write` is the `write` step above (`change_buffer` with `"write"`): the file is loaded or reused, my unsaved changes go to a sidecar, the whole content is set and saved, and one `u` undoes it. `Delete` calls `delete` in `buffer.lua`. If the file's buffer has unsaved changes of mine, they go to a sidecar first, with the same path, timestamp and warning as above. Then the file is removed from disk and its buffer wiped (`nvim_buf_delete` with `force`), so no buffer is left holding a file that no longer exists. Undo can't bring a deleted file back: my unsaved text survives in the sidecar, and the saved version is whatever git has. A delete leaves empty directories in place.
