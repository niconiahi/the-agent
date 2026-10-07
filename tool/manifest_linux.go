package tool

const MANIFEST_SCRIPT = `find . \( -path ./.the-agent -o -name .git \) -prune -o \( -type f -o -type l \) -printf '%i %s %T@ %y%m %p\n'`

const ARCHIVE_SCRIPT = `tar -c -f - --null -T -`
