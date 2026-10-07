package integration_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/niconiahi/the-agent/nvim/nvimtest"
	"github.com/niconiahi/the-agent/session"
)

func new_session(text string) bool {
	created, rest, ok := strings.Cut(strings.TrimPrefix(text, session.SYSTEM_PROMPT_LINK+"\n\n"+session.CREATED_PREFIX), "\n")
	if !ok {
		return false
	}
	if _, error := time.Parse(session.TIMESTAMP_FORMAT, created); error != nil {
		return false
	}
	return rest == "\n## user\n\n"
}

func build_binary(t *testing.T) string {
	t.Helper()
	return build_binary_in(t, t.TempDir())
}

func build_binary_in(t *testing.T, directory string) string {
	t.Helper()
	binary := filepath.Join(directory, "the-agent")
	build := exec.Command("go", "build", "-o", binary, "./cmd/agent")
	build.Dir = nvimtest.RepoRoot()
	if output, error := build.CombinedOutput(); error != nil {
		t.Fatalf("go build: %v\n%s", error, output)
	}
	return binary
}

func TestNvimMode_RunsAsNeovimJob(t *testing.T) {
	harness := nvimtest.Launch(t)

	binary := build_binary(t)
	harness.Setup(`{ bin = ... }`, binary)

	if error := harness.CommandError("TA foo"); error == nil || !strings.Contains(error.Error(), "sudo the-agent setup") {
		t.Fatalf("in a project _the-agent cannot read, :TA must name sudo the-agent setup, got %v", error)
	}
	if _, error := os.Stat(filepath.Join(harness.Dir, ".the-agent")); !errors.Is(error, os.ErrNotExist) {
		t.Fatalf("the refused :TA created .the-agent: %v", error)
	}

	var pid int
	if error := harness.Nvim.ExecLua(`return vim.fn.jobpid(require("the-agent").config.chan)`, &pid); error != nil {
		t.Fatal(error)
	}

	if _, error := exec.LookPath("lsof"); error == nil {
		output, error := exec.Command("lsof", "-a", "-p", strconv.Itoa(pid), "-d", "cwd", "-Fn").Output()
		if error != nil {
			t.Fatalf("lsof: %v", error)
		}
		if !strings.Contains(string(output), "\nn"+harness.Dir+"\n") {
			t.Fatalf("binary cwd is not nvim's (%s):\n%s", harness.Dir, output)
		}
	}

	harness.Nvim.Close()

	deadline := time.Now().Add(5 * time.Second)
	for !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
		if time.Now().After(deadline) {
			t.Fatalf("the-agent (pid %d) outlived its Neovim", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestNvimMode_OpensSessionsInASetUpProject(t *testing.T) {
	current := sandbox_project(t)
	harness := nvimtest.LaunchIn(t, current.project)
	first, second := current.name+"-first", current.name+"-second"
	t.Cleanup(func() {
		for _, name := range []string{first, second} {
			os.RemoveAll(filepath.Join(current.project, ".the-agent", "sessions", name))
		}
	})

	binary := build_binary_in(t, current.directory)
	harness.Setup(`{ bin = ... }`, binary)

	harness.Command("TA " + first)
	if got := harness.ReadFile(".the-agent/sessions/" + first + "/session.md"); !new_session(got) {
		t.Fatalf("session not created by the binary: %q", got)
	}

	other := current.path("elsewhere")
	if error := os.Mkdir(other, 0o755); error != nil {
		t.Fatal(error)
	}
	harness.Command("cd " + other)
	harness.Command("TA " + second)
	if got, want := harness.BufferName(), filepath.Join(harness.Dir, ".the-agent", "sessions", second, "session.md"); got != want {
		t.Fatalf("after :cd the session must stay in the binary's project %q, got %q", want, got)
	}
}
