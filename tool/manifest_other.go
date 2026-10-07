//go:build !linux

package tool

const MANIFEST_SCRIPT = `find . \( -path ./.the-agent -o -name .git \) -prune -o \( -type f -o -type l \) -exec stat -f '%i %z %Fm %p %N' {} +`

const ARCHIVE_SCRIPT = `tar -c -f - --no-mac-metadata --null -T -`
