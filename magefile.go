//go:build mage

package main

import (
	"github.com/magefile/mage/sh"
)

func Build() error {
	return sh.RunV("go", "build", "./...")
}

func Test() error {
	return sh.RunV("go", "test", "./...")
}

func TestVerbose() error {
	return sh.RunV("go", "test", "-v", "./...")
}

func Clean() error {
	return sh.RunV("go", "clean", "./...")
}
