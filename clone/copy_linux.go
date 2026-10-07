package clone

import (
	"io"
	"io/fs"
	"os"

	"golang.org/x/sys/unix"
)

const DIRECTORY_BITS fs.FileMode = 0o770

func copy_file(source string, destination string, info fs.FileInfo) (bool, error) {
	input, error := os.Open(source)
	if error != nil {
		return false, error
	}
	defer input.Close()
	output, error := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if error != nil {
		return false, error
	}
	cloned := unix.IoctlFileClone(int(output.Fd()), int(input.Fd())) == nil
	if !cloned {
		if _, error := io.Copy(output, input); error != nil {
			output.Close()
			return false, error
		}
	}
	if error := output.Close(); error != nil {
		return false, error
	}
	return cloned, finish_copy(destination, info)
}
