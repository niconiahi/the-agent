package clone

import (
	"errors"
	"io/fs"

	"golang.org/x/sys/unix"
)

const DIRECTORY_BITS fs.FileMode = 0o700

func copy_file(source string, destination string, info fs.FileInfo) (bool, error) {
	error := unix.Clonefile(source, destination, unix.CLONE_NOFOLLOW)
	if errors.Is(error, unix.ENOTSUP) || errors.Is(error, unix.EXDEV) {
		return false, plain_copy(source, destination, info)
	}
	if error != nil {
		return false, error
	}
	return true, finish_copy(destination, info)
}
