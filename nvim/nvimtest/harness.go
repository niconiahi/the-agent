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
	"github.com/niconiahi/the-agent/tool"
)

type Harness struct {
	T *testing.T

	Nvim *neovim.Nvim

	Dir string
}

func RepoRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func Start(t *testing.T, config nvim.Config) *Harness {
	t.Helper()
	return StartWithTools(t, config, nil)
}

func StartWithTools(t *testing.T, config nvim.Config, build func(*neovim.Nvim) []tool.Tool) *Harness {
	t.Helper()
	var harness *Harness
	if config.Project == "" {
		harness = Launch(t)
	} else {
		harness = LaunchIn(t, config.Project)
	}
	if build != nil {
		config.Tools = append(append([]tool.Tool{}, config.Tools...), build(harness.Nvim)...)
	}
	if config.Project == "" {
		config.Project = harness.Dir
	}
	if error := nvim.Attach(harness.Nvim, config); error != nil {
		t.Fatalf("attach: %v", error)
	}
	harness.Setup(`{ chan = ... }`, harness.Nvim.ChannelID())
	return harness
}

func Launch(t *testing.T) *Harness {
	t.Helper()
	directory, error := filepath.EvalSymlinks(t.TempDir())
	if error != nil {
		t.Fatal(error)
	}
	return LaunchIn(t, directory)
}

func LaunchIn(t *testing.T, directory string) *Harness {
	t.Helper()
	if _, error := exec.LookPath("nvim"); error != nil {
		t.Skip("nvim is not on PATH")
	}

	client, error := neovim.NewChildProcess(
		neovim.ChildProcessArgs("--embed", "--headless", "--clean", "-n",
			"--cmd", "set rtp^="+RepoRoot()),
		neovim.ChildProcessDir(directory),
		neovim.ChildProcessLogf(t.Logf),
	)
	if error != nil {
		t.Fatalf("start nvim: %v", error)
	}
	t.Cleanup(func() { client.Close() })

	return &Harness{T: t, Nvim: client, Dir: directory}
}

func (harness *Harness) Setup(options string, args ...any) {
	harness.T.Helper()
	if error := harness.Nvim.ExecLua(`require("the-agent").setup(`+options+`)`, nil, args...); error != nil {
		harness.T.Fatalf("plugin setup: %v", error)
	}
}

func (harness *Harness) Command(command string) {
	harness.T.Helper()
	if error := harness.Nvim.Command(command); error != nil {
		harness.T.Fatalf("%s: %v", command, error)
	}
}

func (harness *Harness) CommandError(command string) error {
	return harness.Nvim.Command(command)
}

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

func (harness *Harness) ReadFile(relative string) string {
	harness.T.Helper()
	contents, error := os.ReadFile(filepath.Join(harness.Dir, relative))
	if error != nil {
		harness.T.Fatal(error)
	}
	return string(contents)
}

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
