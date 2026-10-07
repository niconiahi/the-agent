package integration_test

import (
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetup_DryRunPrintsTheCommandsWithoutRoot(t *testing.T) {
	binary := build_binary(t)
	project, error := filepath.EvalSymlinks(t.TempDir())
	if error != nil {
		t.Fatal(error)
	}

	command := exec.Command(binary, "setup", "--dry-run", project)
	output, error := command.CombinedOutput()

	if error != nil {
		t.Fatalf("setup --dry-run: %v\n%s", error, output)
	}
	if !strings.Contains(string(output), "ACL read on "+project+" (inherit)") {
		t.Fatalf("no read ACL step for %s:\n%s", project, output)
	}
	if !strings.Contains(string(output), "    sudo -n -u _the-agent ls "+project+"\n") {
		t.Fatalf("no check command:\n%s", output)
	}
}

func TestSetup_RefusesTheRootFolder(t *testing.T) {
	binary := build_binary(t)

	command := exec.Command(binary, "setup", "--dry-run", "/")
	output, error := command.CombinedOutput()

	var exit *exec.ExitError
	if !errors.As(error, &exit) || exit.ExitCode() != 1 {
		t.Fatalf("want exit status 1, got %v\n%s", error, output)
	}
	if !strings.Contains(string(output), "refusing /") {
		t.Fatalf("no refusal:\n%s", output)
	}
}
