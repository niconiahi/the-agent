package tool

// MANIFEST_SCRIPT lists every file and link in the clone, outside .git and
// .the-agent, as "<inode> <size> <mtime with nanoseconds> <type and mode> <path>"
// with GNU find.
const MANIFEST_SCRIPT = `find . \( -path ./.the-agent -o -name .git \) -prune -o \( -type f -o -type l \) -printf '%i %s %T@ %y%m %p\n'`

// ARCHIVE_SCRIPT streams the NUL-separated paths on stdin as a tar archive.
const ARCHIVE_SCRIPT = `tar -c -f - --null -T -`
