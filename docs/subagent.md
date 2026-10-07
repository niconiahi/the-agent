# subagent

The `task` tool, through which an agent delegates a job to a fresh agent. The child runs in its own `session.md`, in a numbered subfolder of its parent's session, and only its final answer comes back as the tool result. The package sits above `orchestrator` because `tool` can't import `orchestrator` without a cycle, and it reaches the editor only through a `Host`, which `nvim` implements.

`Task(Config)` builds the tool. Its arguments are `job`, what the child should do and report back, and an optional `role`, which fixes the child's tools, picked from `Config.Tools` in their order. An `explorer`, the default, gets only the reading tools (`read`, `grep`, `find`, `ls`, `bash_read`); a `worker` also gets `edit`, `write`, `filter` and `bash_write`. `Config.ExplorerTools` replaces the explorer's list, and a worker's list is built on top of it; the frontend passes `nvim.Config.ExplorerTools` through, which only tests set (to let an explorer edit, so the follow window has a subagent edit to follow). The tool runs only inside a session, which it finds with `vimtool.SessionDirectory`.

## Depth and parallel tasks

An agent's depth is how deep its session is nested, which `Depth(directory)` counts as the ancestor folders that hold a `session.md`: 0 for a root session, 1 for its children. Every agent below `Config.MaxDepth` (`DEFAULT_MAX_DEPTH`, 3, when unset; the frontend passes `nvim.Config.MaxDepth`) gets `task` on top of its tools, so children can delegate too, and an agent at the limit gets no `task` at all. Children run their tool calls in parallel, as root sessions do, so several `task` calls in one turn run their children at the same time and the reports come back in call order.

## Continuing a child

`:TASend` in a child's `session.md` sends it like any session. `Config.ToolsFor(directory)` gives that send the right tools: every tool for a root session, and for a child the tools of the role its parent's `task` call gave it, which it reads back from the parent's `session.md` with `session.LinkedCall` (the call whose result follows the child's link). A child whose call is gone from the parent continues as an explorer, the safer default.

When the continued turn ends without error or abort, the frontend takes the child's new final answer with `Report` and puts it into the direct parent's result with `session.Amend`, which replaces the `tool_result` right after the child's link with one marked `amended=<ts>`. It edits the parent's buffer and saves it when it is loaded, and the file on disk otherwise. If that result was deleted, there is nothing to replace and the parent stays as it is; a parent that is running is not amended, and the user is told.

## The child's session

`create_folder` makes the next numbered subfolder of the parent's session directory, named from the job's first `SLUG_WORDS` words: `01-map-callers`, `02-rewrite-middleware`. Numbers continue from the highest `NN-` folder already there, and `os.Mkdir` fails on a folder that exists, so tasks started at the same time never share one. `write_session` writes the child's `session.md` with `session.NewLinked`, linked to the shared `system_prompt.md` through a relative path (so nesting deeper still works) and holding the job as its first, timestamped user message. The child starts from the system prompt and message read back from that file, exactly as a send of the file would.

The child's agent ID is its session directory, the same identity a root session has. `Host.Open(path)` returns the listener every child event goes to and an `end` called with the error the child stopped on; the nvim host streams the child into its own buffer and routes its events there by that ID, straight from the child, never through the parent agent, which would stamp them with its own ID. The parent's context reaches the child, so `:TAAbort` on the parent aborts the child too. The child holds the write lease on its own `session.md` while it runs, and every lease it takes by editing; `end` releases them all (`vimtool.Release`), so a child's task ending is what frees its files for its siblings and its parent (see `vimtool.md`).

## The result

The tool result's `Details` is a `Link` to the child's folder, whose `String()` is the markdown link the parent's `session.md` gets before the `tool_result` block, so `gf` opens the child. The content is the text of the child's last assistant message, its report. When the child fails or is aborted, the tool returns the error together with the `Link`, and `orchestrator` keeps a failing tool's `Details`, so the parent's file still links the child.
