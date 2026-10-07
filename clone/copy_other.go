//go:build !darwin && !linux

package clone

import "io/fs"

const DIRECTORY_BITS fs.FileMode = 0o700

func copy_file(source string, destination string, info fs.FileInfo) (bool, error) {
	return false, plain_copy(source, destination, info)
}
