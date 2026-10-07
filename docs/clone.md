# clone — `the-agent sync`

`bash_write` runs its command in `<project>/.the-agent/clone`, a copy of the project that `_the-agent` owns (setup creates the folder, see `setup.md`). This package keeps that copy up to date. It is the hidden `the-agent sync <project>` subcommand, which `bash_write` runs as `_the-agent` through the same `sudo -n -u _the-agent` path as every other command, right before listing the clone (see `clone.go` in `tool.md`). It imports nothing internal.

## sync.go

`Path(project)` is `<project>/.the-agent/clone`. `Run(arguments)` is the subcommand: it takes one absolute project path, syncs `Path(project)` and writes nothing to stdout, because stdout carries the listing that `bash_write` runs after it in the same call. `cmd/agent` calls it for `the-agent sync`, and `tool`'s tests call it from their `TestMain`, so the test binary can stand in for the-agent.

`Sync(project, clone) (Report, error)` walks the project folder by folder, comparing each folder's entries with the clone's:

- `.the-agent` at the top of the project is skipped, so the copy never contains itself; `.git` is copied like anything else, so git works in the clone.
- A regular file is copied when the clone has no file there or when its size, nanosecond mtime or mode (permissions plus setuid, setgid and sticky) differ. The old copy is removed first, then the file is copied and given the source's mode and mtime. So a second sync copies only what changed since the first, and whatever a command did to a file in the clone is undone, since the command changed its mtime.
- A symbolic link is recreated as a link when its target differs.
- A folder is created with mode 0777 (the umask, or on Linux the default ACL, narrows it) and walked.
- Whatever the clone has and the project doesn't, or has with another type (a file where the project has a folder), is removed. Sockets, FIFOs and devices are treated as absent.

`Report{Copied, Cloned, Removed}` lists the project-relative paths it copied (files and links), the ones among them it copied with copy-on-write, and the ones it removed.

Every step leaves a state the next sync can finish from: a file copied halfway has the wrong size or mtime, and a missing one is simply missing. So a sync that is interrupted or fails (an unreadable file stops it with an error naming the path) is completed by the next one.

## Copying a file

`copy_file` is per system:

- **macOS** (`copy_darwin.go`): `clonefile(2)` on each file, falling back to a plain copy on `ENOTSUP` or `EXDEV`. Cloning one file at a time matters: a file cloned on its own gets the clone folder's inherited ACL entry, so I can delete it without sudo, while a folder cloned in one call gives the entry to its top only.
- **Linux** (`copy_linux.go`): create the file, then the `FICLONE` ioctl, which shares the blocks on btrfs and XFS; when it fails (ext4, tmpfs, overlayfs), copy the bytes.
- **elsewhere** (`copy_other.go`): a plain copy.

## Folder bits and the Linux ACL mask

Before walking a clone folder, and before removing one, the sync makes sure it has `DIRECTORY_BITS`: 0700 on macOS, 0770 on Linux. The owner bits matter everywhere: a command that ran `chmod 0555` on a folder would otherwise stop the next sync from writing it. The group bits matter on Linux, where a file with an ACL reports its ACL mask as the group bits, and `chmod` sets the mask through them. A folder made by a copy tool, or chmodded by a command, ends up with a mask such as `r-x`, which caps my inherited `rwx` entry and stops me from deleting the clone without sudo; raising the group bits back to `rwx` restores the mask without needing `setfacl`. Files are left with the source's mode, because their group bits are part of the mode the listing compares and `bash_write` replays, and deleting a file only needs rights on its folder.

## Tests

`clone/sync_test.go` runs as me on temporary folders: the first sync copies the project with `.git` and without `.the-agent`; a second copies only the files whose size, mtime or mode changed; edits, new files and folders, a read-only folder and a file turned into a folder are undone; links are copied as links; a sync stopped by an unreadable file is completed by the next; and files are cloned on APFS and btrfs and plainly copied on ext4, tmpfs and overlayfs (`filesystem_*_test.go` decide which applies, and the test skips on a filesystem they don't know). Deleting what the sync copied as real `_the-agent` is `TestBashWrite_ICanDeleteWhatTheSyncCopiedIntoTheClone` in `integration/run_commands_as_the_agent_test.go`, which needs a set-up project.
