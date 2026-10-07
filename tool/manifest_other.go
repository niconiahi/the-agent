//go:build !linux

package tool

// MANIFEST_SCRIPT lists every file and link in the clone, outside .git and
// .the-agent, as "<inode> <size> <mtime with nanoseconds> <type and mode> <path>"
// with BSD stat.
const MANIFEST_SCRIPT = `find . \( -path ./.the-agent -o -name .git \) -prune -o \( -type f -o -type l \) -exec stat -f '%i %z %Fm %p %N' {} +`

// ARCHIVE_SCRIPT streams the NUL-separated paths on stdin as a tar archive,
// without AppleDouble entries for extended attributes.
const ARCHIVE_SCRIPT = `tar -c -f - --no-mac-metadata --null -T -`
