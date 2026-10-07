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
3. **caches** `~_the-agent/{gocache,gomodcache,tmp,clones}`, owned by `_the-agent`, home mode 0700.
4. **ACL read on the project (inherit)**: `chmod -R +a '_the-agent allow read,execute,readattr,readextattr,readsecurity,file_inherit,directory_inherit'` on macOS (on a directory `read` and `execute` are `list` and `search`; adding the same entry twice keeps one), or `setfacl -R -m u:_the-agent:rX` plus a default entry on every directory on Linux. The project is also appended to `~_the-agent/projects`, which is how uninstall knows every project.
5. **search on the parent folders**, from my home down to the project's parent (or up to `/`, exclusive, for a project outside my home): `chmod +a '_the-agent allow search'` or `setfacl -m u:_the-agent:x`. Search lets `_the-agent` pass through a folder without listing it.
6. **check**: `sudo -n -u _the-agent ls <project>` prints `→ ready`.

Steps 1–3 are done once per machine; on a later project only 4–6 have work.

## Flags

- `--dry-run` runs the probes, which only read, and prints each pending step's commands instead of running them. It needs no root.
- `--check` runs only the check. On macOS, `Operation not permitted` means TCC is blocking `_the-agent` despite the ACLs (typically under `~/Documents`), and the error says to allow the app Neovim runs in under System Settings → Privacy & Security → Full Disk Access. Any other failure says to run setup.
- `--uninstall` removes the project's ACL entries and its line in `~_the-agent/projects`, and the search entries on parent folders that no other recorded project still needs. Its steps print `absent` or `removed`.
- `--uninstall --all` removes every recorded project's ACLs and their parents' search, then `~_the-agent`, the sudoers file and the user (and its macOS group).

Without root, anything but `--dry-run` and `--check` stops with `ERROR_NOT_ROOT` before running a command.

## Testing

`Host` carries everything machine-specific: the system (`darwin` or `linux`), the invoking user and home, the working directory, whether it is root, the `Shell` and the output. `Local` builds it for this machine. The tests in `setup/setup_test.go` use a fake `Shell` that answers probes from a table, so they cover both systems' command lists, idempotence, the rejected sudoers draft, the check messages and both uninstall scopes without root; `integration/set_up_the_agent_sandbox_test.go` runs the real binary's dry run and refusal.
