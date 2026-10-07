//go:build !darwin && !linux

package clone_test

import "testing"

func copy_on_write_filesystem(t *testing.T, directory string) (bool, bool) {
	return false, true
}
