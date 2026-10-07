# setup — `the-agent setup`

Every shell command the agent runs will run as `_the-agent`, an unprivileged user that can read the project but not write it (see "The shell: `_the-agent`" in `VIM_NATIVE_DESIGN.md`). This package prepares a machine and a project for that, once, as root:

```
sudo the-agent setup [--dry-run] [--check | --uninstall [--all]] [project]
```

The project defaults to the working directory. It must be a directory, and it can't be `/`, my home, or a folder that contains my home, since `_the-agent` would then read everything in it.

## Steps

Setup is a list of steps, each with a probe that tells whether it is already done and the commands that do it. A done step prints `✓ <step> (exists)`; otherwise its commands run in order and it prints `✓ <step> (created)`, or stops at the first failing command with that command's output. So a second run prints `exists` for every step and runs no command that changes anything.

1. **user `_the-agent`**, probed with `id -u`. On macOS a group and a hidden user are created with `dscl`, with the first id between 400 and 499 that no user or group has, shell `/usr/bin/false`, password `*` (no login) and home `/var/the-agent`. On Linux it is `useradd --system --user-group` with home `/var/lib/the-agent`, whose system users get a locked password.
2. **`/etc/sudoers.d/the-agent`**, probed with `test -f`. The line `<me> ALL=(_the-agent) NOPASSWD: ALL`, where `<me>` is `SUDO_USER`, is written to `/etc/sudoers.d/the-agent.tmp`, made 0440, checked with `visudo -cf`, and only then moved into place. If `visudo` rejects it, the draft is removed and nothing is installed. sudo ignores files with a `.` in their name, so the draft is never read as configuration.
3. **caches** `~_the-agent/{gocache,gomodcache}`, owned by `_the-agent`, home mode 0700. Nothing else lives in `_the-agent`'s home: its temporary files and its copy of the project are in the project.
4. **ACL read on the project (inherit)**: `chmod -R +a '_the-agent allow read,execute,readattr,readextattr,readsecurity,file_inherit,directory_inherit'` on macOS (on a directory `read` and `execute` are `list` and `search`; adding the same entry twice keeps one), or `setfacl -R -m u:_the-agent:rX` plus a default entry on every directory on Linux. The project is also appended to `~_the-agent/projects`, which is how uninstall knows every project.
5. **`.the-agent/{clone,tmp}` owned by `_the-agent`, full control for me**, probed with `ls -lde` (owner `_the-agent` and my entry) or `getfacl -p` (owner, `user:<me>:rwx`, `default:user:<me>:rwx` and both masks `rwx`). `.the-agent/` is created first if missing and given to me (`chown <me>:`). Then `clone` and `tmp` are created, given to `_the-agent`, made 0700, and get an inheritable full-control entry for me: `chmod +a '<me> allow list,add_file,search,delete,add_subdirectory,delete_child,readattr,writeattr,readextattr,writeextattr,readsecurity,writesecurity,chown,file_inherit,directory_inherit'` on macOS, `setfacl -m u:<me>:rwx,d:u:<me>:rwx,m::rwx,d:m::rwx` on Linux (after the `chmod`, since on Linux the group bits are the ACL mask). Whatever `_the-agent` creates in them inherits the entry, so I can delete it without sudo. `clone` is `bash_write`'s copy of the project, `tmp` is the `TMPDIR` of both bash tools. On Linux the inheritance holds for files `_the-agent` creates natively; a copy tool requests the source's mode, which becomes the new file's ACL mask and caps my entry (a 0644 source gives me `r--`), so whatever copies into the clone has to restore the mask itself (measured in `acl-inheritance` notes; #27's sync does it).
6. **search on the parent folders**, from my home down to the project's parent (or up to `/`, exclusive, for a project outside my home): `chmod +a '_the-agent allow search'` or `setfacl -m u:_the-agent:x`. Search lets `_the-agent` pass through a folder without listing it.
7. **search on the folders holding the binary**, the same entries on the folders from my home (or `/`) down to the folder holding the-agent's binary (`os.Executable`, symlinks resolved), so `_the-agent` can run it. Folders anyone can already search (mode `o+x`, such as `/usr/local/bin`, where macOS wouldn't even take an ACL) and folders step 6 covers are skipped. When nothing is left the step prints `✓ search on the folders holding <binary> (exists)`.
8. **check**, four probes as `_the-agent`: `sudo -n -u _the-agent ls <project>`, `test -x <binary>`, `test -w <project>/.the-agent/clone` and `test -w <project>/.the-agent/tmp`. Each passing probe prints `✓ check: <probe>`, then `ready`.

Steps 1–3 are done once per machine; on a later project steps 4–6 and 8 have work, and step 7 only if the binary moved.

## Flags

- `--dry-run` runs the probes, which only read, and prints each pending step's commands instead of running them. It needs no root.
- `--check` runs only the check. On macOS, `Operation not permitted` from the `ls` probe means TCC is blocking `_the-agent` despite the ACLs (typically under `~/Documents`), and the error says to allow the app Neovim runs in under System Settings → Privacy & Security → Full Disk Access. Any other failure says to run setup: `_the-agent cannot run <binary>: run sudo the-agent setup <project>` when the binary probe fails, `_the-agent cannot write <project>/.the-agent/clone: …` for the clone or tmp.
- `--uninstall` removes the project's `.the-agent/clone` and `.the-agent/tmp` (`rm -rf`), its ACL entries and its line in `~_the-agent/projects`, and the search entries on parent folders that no other recorded project, nor the binary, still needs. Its steps print `absent` or `removed`.
- `--uninstall --all` removes every recorded project's clone, tmp and ACLs, their parents' search and the search on the binary's folders, then `~_the-agent`, the sudoers file and the user (and its macOS group). The binary is the one running the uninstall; search granted for a binary that has since moved is left behind.

Without root, anything but `--dry-run` and `--check` stops with `ERROR_NOT_ROOT` before running a command.

## Used by the agent

`Ready(project, binary)` is the check on its own, run directly on this machine with the same probes and diagnosis. The `--nvim` binary sets it, with its own path from `Binary()`, as `nvim.Config.Sandbox`, so `:TA` in a project that isn't set up, or with a binary `_the-agent` can't run, shows that error and creates nothing. `Home()` is `~_the-agent` on this system; `cmd/agent` passes it, with `USER` and the project, as the `tool.Sandbox` that `bash_read` runs commands in, with `TMPDIR` at `<project>/.the-agent/tmp`. `bash_write` will keep its persistent copy in `<project>/.the-agent/clone`, brought up to date by a `the-agent sync` run as `_the-agent` (#27), which is why `_the-agent` must be able to run the binary; until then the macOS cloner still clones into `~_the-agent/clones`, creating that folder itself (see `clonefile.go` in `tool.md`).

## Testing

`Host` carries everything machine-specific: the system (`darwin` or `linux`), the invoking user and home, the working directory, the binary, whether it is root, the `Shell` and the output. `Local` builds it for this machine. The tests in `setup/setup_test.go` use a fake `Shell` that answers probes from a table, so they cover both systems' command lists, idempotence, the rejected sudoers draft, the check messages and both uninstall scopes without root; `integration/set_up_the_agent_sandbox_test.go` runs the real binary's dry run and refusal. `TestSetUp_ICanDeleteWhatTheAgentCreatesInTheCloneAndTmp` in `integration/run_commands_as_the_agent_test.go` needs a machine where setup has run on the repository: `_the-agent` creates nested files in both folders and I delete them without sudo. It was run as root-free user `tester` in a Debian trixie container after a real `the-agent setup`; macOS's real setup has not been exercised.
