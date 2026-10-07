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
	switch status.Type {
	case unix.BTRFS_SUPER_MAGIC:
		return true, true
	case unix.EXT4_SUPER_MAGIC, unix.TMPFS_MAGIC, unix.OVERLAYFS_SUPER_MAGIC:
		return false, true
	}
	return false, false
}
