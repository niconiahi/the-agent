# subagent

The `task` tool, through which an agent delegates a job to a fresh agent. The child runs in its own `session.md`, in a numbered subfolder of its parent's session, and only its final answer comes back as the tool result. The package sits above `orchestrator` because `tool` can't import `orchestrator` without a cycle, and it reaches the editor only through a `Host`, which `nvim` implements.

`Task(Config)` builds the tool. Its arguments are `job`, what the child should do and report back, and an optional `role`; the only role so far is `explorer`, which gets only the reading tools (`read`, `grep`, `find`, `ls`, `bash_read`), picked from `Config.Tools` in their order. `Config.ExplorerTools` replaces that list; the frontend passes `nvim.Config.ExplorerTools` through, which only tests set (to let an explorer edit, so the follow window has a subagent edit to follow). The tool runs only inside a session, which it finds with `vimtool.SessionDirectory`.

## The child's session

`create_folder` makes the next numbered subfolder of the parent's session directory, named from the job's first `SLUG_WORDS` words: `01-map-callers`, `02-rewrite-middleware`. Numbers continue from the highest `NN-` folder already there, and `os.Mkdir` fails on a folder that exists, so tasks started at the same time never share one. `write_session` writes the child's `session.md` with `session.NewLinked`, linked to the shared `system_prompt.md` through a relative path (so nesting deeper still works) and holding the job as its first, timestamped user message. The child starts from the system prompt and message read back from that file, exactly as a send of the file would.

The child's agent ID is its session directory, the same identity a root session has. `Host.Open(path)` returns the listener every child event goes to and an `end` called with the error the child stopped on; the nvim host streams the child into its own buffer and routes its events there by that ID, straight from the child, never through the parent agent, which would stamp them with its own ID. The parent's context reaches the child, so `:TAAbort` on the parent aborts the child too.

## The result

The tool result's `Details` is a `Link` to the child's folder, whose `String()` is the markdown link the parent's `session.md` gets before the `tool_result` block, so `gf` opens the child. The content is the text of the child's last assistant message, its report. When the child fails or is aborted, the tool returns the error together with the `Link`, and `orchestrator` keeps a failing tool's `Details`, so the parent's file still links the child.
