// Package nvimtest is the end-to-end test harness for the Neovim frontend.
//
// Start launches `nvim --embed --headless --clean` in a temp project
// directory with this repository on the runtimepath (so plugin/ and lua/ are
// loaded exactly as a user would get them), attaches the nvim package to that
// Neovim in-process, and points the Lua plugin at the test's RPC channel.
// Because the Go side runs in the test process, a fake provider registered
// with RegisterProvider is what :TASend talks to.
package nvimtest

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	neovim "github.com/neovim/go-client/nvim"

	"github.com/niconiahi/the-agent/nvim"
)

type Harness struct {
	T *testing.T
	// Nvim is the RPC client of the embedded Neovim, for anything the helpers
	// below don't cover.
	Nvim *neovim.Nvim
	// Dir is the project directory and Neovim's working directory, with
	// symlinks resolved (on macOS t.TempDir() lives under a /var symlink).
	Dir string
}

// RepoRoot is the root of this repository, which is also the plugin's root.
func RepoRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

// Start launches a headless Neovim with the plugin loaded and the Go side
// attached in-process with config. It skips the test when nvim is not
// installed.
func Start(t *testing.T, config nvim.Config) *Harness {
	t.Helper()
	harness := Launch(t)
	if error := nvim.Attach(harness.Nvim, config); error != nil {
		t.Fatalf("attach: %v", error)
	}
	harness.Setup(`{ chan = ... }`, harness.Nvim.ChannelID())
	return harness
}

// Launch starts a headless Neovim with the plugin on the runtimepath but
// does not attach the Go side or configure the plugin. Use it with Setup to
// drive a real `the-agent --nvim` binary.
func Launch(t *testing.T) *Harness {
	t.Helper()
	if _, error := exec.LookPath("nvim"); error != nil {
		t.Skip("nvim is not on PATH")
	}

	dir, error := filepath.EvalSymlinks(t.TempDir())
	if error != nil {
		t.Fatal(error)
	}

	client, error := neovim.NewChildProcess(
		neovim.ChildProcessArgs("--embed", "--headless", "--clean", "-n",
			"--cmd", "set rtp^="+RepoRoot()),
		neovim.ChildProcessDir(dir),
		neovim.ChildProcessLogf(t.Logf),
	)
	if error != nil {
		t.Fatalf("start nvim: %v", error)
	}
	t.Cleanup(func() { client.Close() })

	return &Harness{T: t, Nvim: client, Dir: dir}
}

// Setup calls require("the-agent").setup(<options>), where options is a Lua
// table expression that can refer to args as `...`.
func (harness *Harness) Setup(options string, args ...any) {
	harness.T.Helper()
	if error := harness.Nvim.ExecLua(`require("the-agent").setup(`+options+`)`, nil, args...); error != nil {
		harness.T.Fatalf("plugin setup: %v", error)
	}
}

// Command runs an Ex command and fails the test if it errors.
func (harness *Harness) Command(command string) {
	harness.T.Helper()
	if error := harness.Nvim.Command(command); error != nil {
		harness.T.Fatalf("%s: %v", command, error)
	}
}

// CommandError runs an Ex command and returns its error.
func (harness *Harness) CommandError(command string) error {
	return harness.Nvim.Command(command)
}

// BufferName is the full path of the current buffer.
func (harness *Harness) BufferName() string {
	harness.T.Helper()
	buffer, error := harness.Nvim.CurrentBuffer()
	if error != nil {
		harness.T.Fatal(error)
	}
	name, error := harness.Nvim.BufferName(buffer)
	if error != nil {
		harness.T.Fatal(error)
	}
	return name
}

// Text is the current buffer as it would be written to disk.
func (harness *Harness) Text() string {
	harness.T.Helper()
	buffer, error := harness.Nvim.CurrentBuffer()
	if error != nil {
		harness.T.Fatal(error)
	}
	lines, error := harness.Nvim.BufferLines(buffer, 0, -1, true)
	if error != nil {
		harness.T.Fatal(error)
	}
	joined := make([]string, len(lines))
	for index, line := range lines {
		joined[index] = string(line)
	}
	return strings.Join(joined, "\n") + "\n"
}

// SetText replaces the current buffer's contents with text.
func (harness *Harness) SetText(text string) {
	harness.T.Helper()
	buffer, error := harness.Nvim.CurrentBuffer()
	if error != nil {
		harness.T.Fatal(error)
	}
	lines := [][]byte{}
	for _, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		lines = append(lines, []byte(line))
	}
	if error := harness.Nvim.SetBufferLines(buffer, 0, -1, true, lines); error != nil {
		harness.T.Fatal(error)
	}
}

// WriteFile writes a file relative to the project directory.
func (harness *Harness) WriteFile(relative string, contents string) {
	harness.T.Helper()
	path := filepath.Join(harness.Dir, relative)
	if error := os.MkdirAll(filepath.Dir(path), 0o755); error != nil {
		harness.T.Fatal(error)
	}
	if error := os.WriteFile(path, []byte(contents), 0o644); error != nil {
		harness.T.Fatal(error)
	}
}

// ReadFile reads a file relative to the project directory.
func (harness *Harness) ReadFile(relative string) string {
	harness.T.Helper()
	contents, error := os.ReadFile(filepath.Join(harness.Dir, relative))
	if error != nil {
		harness.T.Fatal(error)
	}
	return string(contents)
}

// WaitFor polls condition until it holds, failing the test after 5s.
func (harness *Harness) WaitFor(description string, condition func() bool) {
	harness.T.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			harness.T.Fatalf("timed out waiting for %s", description)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
