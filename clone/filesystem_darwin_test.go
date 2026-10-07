package clone_test

import (
	"testing"

	"golang.org/x/sys/unix"
)

func copy_on_write_filesystem(t *testing.T, directory string) (bool, bool) {
	t.Helper()
	var status unix.Statfs_t
	if error := unix.Statfs(directory, &status); error != nil {
		t.Fatal(error)
	}
	switch unix.ByteSliceToString(status.Fstypename[:]) {
	case "apfs":
		return true, true
	case "hfs", "msdos", "exfat", "tmpfs":
		return false, true
	}
	return false, false
}
