package main

import (
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWithoutNvimFlag_PrintsUsageAndExits(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "the-agent")
	if output, error := exec.Command("go", "build", "-o", binary, ".").CombinedOutput(); error != nil {
		t.Fatalf("go build: %v\n%s", error, output)
	}

	command := exec.Command(binary)
	command.Dir = t.TempDir()
	command.Env = []string{"KIMI_API_KEY=unused"}
	output, error := command.CombinedOutput()

	var exit *exec.ExitError
	if !errors.As(error, &exit) || exit.ExitCode() != 2 {
		t.Fatalf("want exit status 2, got %v\n%s", error, output)
	}
	if !strings.Contains(string(output), ":TA") || !strings.Contains(string(output), "--nvim") {
		t.Fatalf("usage does not point at Neovim:\n%s", output)
	}
}
