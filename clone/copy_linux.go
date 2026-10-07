package clone

import (
	"io"
	"io/fs"
	"os"

	"golang.org/x/sys/unix"
)

// DIRECTORY_BITS includes the group bits because, on a file with an ACL, they
// are the ACL mask: a folder made by copying a 0755 one gets mask r-x, which
// caps my inherited rwx entry and stops me deleting the clone without sudo.
const DIRECTORY_BITS fs.FileMode = 0o770

// copy_file shares the source's blocks with FICLONE where the filesystem
// supports it (btrfs, XFS) and copies the bytes otherwise (ext4, tmpfs).
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
