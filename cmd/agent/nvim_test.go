package main

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

	"github.com/niconiahi/the-agent/nvim"
	"github.com/niconiahi/the-agent/nvim/nvimtest"
)

func TestNvimMode_RunsAsNeovimJob(t *testing.T) {
	harness := nvimtest.Launch(t)

	binary := filepath.Join(t.TempDir(), "the-agent")
	build := exec.Command("go", "build", "-o", binary, ".")
	if output, error := build.CombinedOutput(); error != nil {
		t.Fatalf("go build: %v\n%s", error, output)
	}
	harness.Setup(`{ bin = ... }`, binary)

	harness.Command("TA foo")
	if got := harness.ReadFile(".the-agent/sessions/foo/session.md"); got != nvim.NEW_SESSION {
		t.Fatalf("session not created by the binary: %q", got)
	}

	other := filepath.Join(harness.Dir, "elsewhere")
	if error := os.Mkdir(other, 0o755); error != nil {
		t.Fatal(error)
	}
	harness.Command("cd " + other)
	harness.Command("TA bar")
	if got, want := harness.BufferName(), filepath.Join(harness.Dir, ".the-agent", "sessions", "bar", "session.md"); got != want {
		t.Fatalf("after :cd the session must stay in the binary's project %q, got %q", want, got)
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
