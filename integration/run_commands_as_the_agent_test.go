package integration_test

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	neovim "github.com/neovim/go-client/nvim"

	"github.com/niconiahi/the-agent/message"
	"github.com/niconiahi/the-agent/nvim/nvimtest"
	"github.com/niconiahi/the-agent/setup"
	"github.com/niconiahi/the-agent/tool"
	"github.com/niconiahi/the-agent/vimtool"
)

type sandbox struct {
	project   string
	directory string
	name      string
}

func (current sandbox) path(name string) string {
	return filepath.Join(current.directory, name)
}

func (current sandbox) relative(name string) string {
	return filepath.Join(current.name, name)
}

func set_up_project(t *testing.T) string {
	t.Helper()
	project, error := filepath.EvalSymlinks(nvimtest.RepoRoot())
	if error != nil {
		t.Fatal(error)
	}
	for _, name := range []string{"clone", "tmp"} {
		directory := filepath.Join(project, ".the-agent", name)
		if output, error := exec.Command("sudo", "-n", "-u", setup.USER, "test", "-w", directory).CombinedOutput(); error != nil {
			t.Skipf("%s cannot write %s (%s): run sudo the-agent setup %s to enable this test", setup.USER, directory, strings.TrimSpace(string(output)), project)
		}
	}
	return project
}

func sandbox_project(t *testing.T) sandbox {
	t.Helper()
	project := set_up_project(t)
	directory, error := os.MkdirTemp(project, ".sandbox-test-")
	if error != nil {
		t.Fatal(error)
	}
	t.Cleanup(func() {
		os.RemoveAll(directory)
		os.RemoveAll(filepath.Join(project, ".the-agent", "sessions", filepath.Base(directory)))
	})
	if error := os.Chmod(directory, 0o755); error != nil {
		t.Fatal(error)
	}
	probe := filepath.Join(directory, "probe.txt")
	if error := os.WriteFile(probe, []byte("probe\n"), 0o644); error != nil {
		t.Fatal(error)
	}
	if output, error := exec.Command("sudo", "-n", "-u", setup.USER, "test", "-r", probe).CombinedOutput(); error != nil {
		t.Skipf("%s cannot read %s (%s): run sudo the-agent setup %s to enable this test", setup.USER, directory, strings.TrimSpace(string(output)), project)
	}
	return sandbox{project: project, directory: directory, name: filepath.Base(directory)}
}

func start_in_sandbox(t *testing.T, current sandbox, calls ...message.ToolCall) (*nvimtest.Harness, *nvimtest.Provider) {
	t.Helper()
	binary := build_binary_in(t, current.directory)
	config := nvimtest.Config()
	config.Project = current.project
	config.Sandbox = func(project string) error {
		return setup.Ready(project, binary)
	}
	replies := []nvimtest.Reply{}
	for _, call := range calls {
		replies = append(replies, nvimtest.Reply{ToolCalls: []message.ToolCall{call}})
	}
	replies = append(replies, nvimtest.Text("done", 10))
	provider := nvimtest.RegisterProvider(t, replies...)
	harness := nvimtest.StartWithTools(t, config, func(client *neovim.Nvim) []tool.Tool {
		sandbox := tool.Sandbox{User: setup.USER, Home: setup.Home(), Project: current.project}
		cloner := tool.Clonefile(current.project, filepath.Join(sandbox.Home, "clones"), sandbox.Command)
		return []tool.Tool{tool.BashReadTool(sandbox), tool.BashWriteTool(sandbox, cloner, vimtool.Replay(client))}
	})
	return harness, provider
}

func TestSetUp_ICanDeleteWhatTheAgentCreatesInTheCloneAndTmp(t *testing.T) {
	project := set_up_project(t)
	for _, name := range []string{"clone", "tmp"} {
		directory := filepath.Join(project, ".the-agent", name, fmt.Sprintf(".deletion-test-%d", os.Getpid()))
		script := `mkdir -p "$1/nested/deeper" && echo theirs > "$1/nested/deeper/file.txt" && echo theirs > "$1/top.txt"`
		if output, error := exec.Command("sudo", "-n", "-u", setup.USER, "/bin/bash", "-c", script, "bash", directory).CombinedOutput(); error != nil {
			t.Fatalf("%s could not create files in %s: %v\n%s", setup.USER, directory, error, output)
		}

		if error := os.RemoveAll(directory); error != nil {
			t.Fatalf("cannot delete what %s created in .the-agent/%s: %v", setup.USER, name, error)
		}
		if _, error := os.Stat(directory); !errors.Is(error, os.ErrNotExist) {
			t.Fatalf("%s is still there: %v", directory, error)
		}
	}
}

func TestBashRead_WriteIntoTheProjectIsPermissionDeniedAndChangesNothing(t *testing.T) {
	current := sandbox_project(t)
	if error := os.WriteFile(current.path("kept.txt"), []byte("mine\n"), 0o644); error != nil {
		t.Fatal(error)
	}
	harness, provider := start_in_sandbox(t, current,
		call("tc_1", "bash_read", map[string]any{"command": "cd " + current.name + " && { echo theirs > kept.txt; touch new.txt; }"}),
	)

	send_agent_turn_in(harness, current.name)

	results := tool_results(t, provider)
	if len(results) != 1 || !strings.Contains(result_text(results[0]), "Permission denied") {
		t.Fatalf("want the model to see Permission denied, got %+v", results)
	}
	if got := harness.ReadFile(current.relative("kept.txt")); got != "mine\n" {
		t.Fatalf("bash_read changed kept.txt: %q", got)
	}
	if _, error := os.Stat(current.path("new.txt")); !errors.Is(error, os.ErrNotExist) {
		t.Fatalf("bash_read created new.txt: %v", error)
	}
}

func TestBashRead_WritesToTheProjectsTmp(t *testing.T) {
	current := sandbox_project(t)
	harness, provider := start_in_sandbox(t, current,
		call("tc_1", "bash_read", map[string]any{"command": `file="$TMPDIR/bash-read-$$"; echo written > "$file" && cat "$file" && rm "$file" && echo "tmpdir=$TMPDIR"`}),
	)

	send_agent_turn_in(harness, current.name)

	text := result_text(tool_results(t, provider)[0])
	if !strings.Contains(text, "written\n") {
		t.Fatalf("the write to TMPDIR failed: %q", text)
	}
	if want := "tmpdir=" + filepath.Join(current.project, ".the-agent", "tmp"); !strings.Contains(text, want) {
		t.Fatalf("want %s, got %q", want, text)
	}
}

func TestBashRead_RunsGoTestWithTheAgentsCaches(t *testing.T) {
	current := sandbox_project(t)
	if _, error := exec.LookPath("go"); error != nil {
		t.Skip("go is not on PATH")
	}
	files := map[string]string{
		"go.mod":         "module sandboxed\n\ngo 1.21\n",
		"answer_test.go": "package sandboxed\n\nimport \"testing\"\n\nfunc TestAnswer(t *testing.T) {\n\tif 6*7 != 42 {\n\t\tt.Fatal(\"arithmetic\")\n\t}\n}\n",
	}
	for name, contents := range files {
		if error := os.WriteFile(current.path(name), []byte(contents), 0o644); error != nil {
			t.Fatal(error)
		}
	}
	harness, provider := start_in_sandbox(t, current,
		call("tc_1", "bash_read", map[string]any{"command": "cd " + current.name + " && go test ./... && go env GOCACHE GOMODCACHE"}),
	)

	send_agent_turn_in(harness, current.name)

	text := result_text(tool_results(t, provider)[0])
	if !strings.Contains(text, "ok  \tsandboxed") {
		t.Fatalf("go test did not pass: %q", text)
	}
	for _, cache := range []string{"gocache", "gomodcache"} {
		if want := filepath.Join(setup.Home(), cache) + "\n"; !strings.Contains(text, want) {
			t.Fatalf("want go to use %s, got %q", want, text)
		}
	}
}

func TestBashWrite_AppliesWhatTheAgentWroteInItsCloneAsAnUndoableEdit(t *testing.T) {
	current := sandbox_project(t)
	if runtime.GOOS != "darwin" {
		t.Skip("bash_write clones with clonefile on macOS only")
	}
	if error := os.WriteFile(current.path("kept.txt"), []byte("one\n"), 0o644); error != nil {
		t.Fatal(error)
	}
	harness, provider := start_in_sandbox(t, current,
		call("tc_1", "bash_write", map[string]any{"command": "echo two >> " + current.relative("kept.txt") + " && pwd -P"}),
		call("tc_2", "bash_write", map[string]any{"command": "pwd -P"}),
	)
	path := current.path("kept.txt")
	harness.Command("edit " + path)

	send_agent_turn_in(harness, current.name)

	results := tool_results(t, provider)
	first, second := result_text(results[0]), result_text(results[1])
	if results[0].IsError || !strings.HasSuffix(first, "Applied to the project:\nM "+current.relative("kept.txt")) {
		t.Fatalf("bash_write result: %q", first)
	}
	clone := filepath.Base(tool.ClonePath(current.project, filepath.Join(setup.Home(), "clones")))
	if !strings.Contains(first, clone+"\n") || strings.SplitN(first, "\n", 2)[0] != strings.SplitN(second, "\n", 2)[0] {
		t.Fatalf("want both commands run in the clone %s, got %q and %q", clone, first, second)
	}
	if got := buffer_lines(harness, path); got != "one\ntwo\n" {
		t.Fatalf("buffer: %q", got)
	}
	if got := harness.ReadFile(current.relative("kept.txt")); got != "one\ntwo\n" {
		t.Fatalf("disk: %q", got)
	}
	undo(harness, path)
	if got := buffer_lines(harness, path); got != "one\n" {
		t.Fatalf("after one undo: %q", got)
	}
}
