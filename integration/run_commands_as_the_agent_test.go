package integration_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	neovim "github.com/neovim/go-client/nvim"

	"github.com/niconiahi/the-agent/message"
	"github.com/niconiahi/the-agent/nvim/nvimtest"
	"github.com/niconiahi/the-agent/setup"
	"github.com/niconiahi/the-agent/tool"
)

func sandbox_project(t *testing.T) string {
	t.Helper()
	root, error := filepath.EvalSymlinks(nvimtest.RepoRoot())
	if error != nil {
		t.Fatal(error)
	}
	project, error := os.MkdirTemp(root, ".sandbox-test-")
	if error != nil {
		t.Fatal(error)
	}
	t.Cleanup(func() { os.RemoveAll(project) })
	probe := filepath.Join(project, "probe.txt")
	if error := os.WriteFile(probe, []byte("probe\n"), 0o644); error != nil {
		t.Fatal(error)
	}
	if output, error := exec.Command("sudo", "-n", "-u", setup.USER, "test", "-r", probe).CombinedOutput(); error != nil {
		t.Skipf("%s cannot read %s (%s): run sudo the-agent setup %s to enable this test", setup.USER, project, strings.TrimSpace(string(output)), root)
	}
	return project
}

func start_in_sandbox(t *testing.T, project string, calls ...message.ToolCall) (*nvimtest.Harness, *nvimtest.Provider) {
	t.Helper()
	config := nvimtest.Config()
	config.Project = project
	config.Sandbox = setup.Ready
	replies := []nvimtest.Reply{}
	for _, call := range calls {
		replies = append(replies, nvimtest.Reply{ToolCalls: []message.ToolCall{call}})
	}
	replies = append(replies, nvimtest.Text("done", 10))
	provider := nvimtest.RegisterProvider(t, replies...)
	harness := nvimtest.StartWithTools(t, config, func(*neovim.Nvim) []tool.Tool {
		return []tool.Tool{tool.BashReadTool(tool.Sandbox{User: setup.USER, Home: setup.Home(), Project: project})}
	})
	return harness, provider
}

func TestBashRead_WriteIntoTheProjectIsPermissionDeniedAndChangesNothing(t *testing.T) {
	project := sandbox_project(t)
	if error := os.WriteFile(filepath.Join(project, "kept.txt"), []byte("mine\n"), 0o644); error != nil {
		t.Fatal(error)
	}
	harness, provider := start_in_sandbox(t, project,
		call("tc_1", "bash_read", map[string]any{"command": "echo theirs > kept.txt; touch new.txt"}),
	)

	send_agent_turn(harness)

	results := tool_results(t, provider)
	if len(results) != 1 || !strings.Contains(result_text(results[0]), "Permission denied") {
		t.Fatalf("want the model to see Permission denied, got %+v", results)
	}
	if got := harness.ReadFile("kept.txt"); got != "mine\n" {
		t.Fatalf("bash_read changed kept.txt: %q", got)
	}
	if _, error := os.Stat(filepath.Join(project, "new.txt")); !errors.Is(error, os.ErrNotExist) {
		t.Fatalf("bash_read created new.txt: %v", error)
	}
}

func TestBashRead_WritesToTheAgentsTmpdir(t *testing.T) {
	project := sandbox_project(t)
	harness, provider := start_in_sandbox(t, project,
		call("tc_1", "bash_read", map[string]any{"command": `file="$TMPDIR/bash-read-$$"; echo written > "$file" && cat "$file" && rm "$file" && echo "tmpdir=$TMPDIR"`}),
	)

	send_agent_turn(harness)

	text := result_text(tool_results(t, provider)[0])
	if !strings.Contains(text, "written\n") {
		t.Fatalf("the write to TMPDIR failed: %q", text)
	}
	if want := "tmpdir=" + filepath.Join(setup.Home(), "tmp"); !strings.Contains(text, want) {
		t.Fatalf("want %s, got %q", want, text)
	}
}

func TestBashRead_RunsGoTestWithTheAgentsCaches(t *testing.T) {
	project := sandbox_project(t)
	if _, error := exec.LookPath("go"); error != nil {
		t.Skip("go is not on PATH")
	}
	files := map[string]string{
		"go.mod":         "module sandboxed\n\ngo 1.21\n",
		"answer_test.go": "package sandboxed\n\nimport \"testing\"\n\nfunc TestAnswer(t *testing.T) {\n\tif 6*7 != 42 {\n\t\tt.Fatal(\"arithmetic\")\n\t}\n}\n",
	}
	for name, contents := range files {
		if error := os.WriteFile(filepath.Join(project, name), []byte(contents), 0o644); error != nil {
			t.Fatal(error)
		}
	}
	harness, provider := start_in_sandbox(t, project,
		call("tc_1", "bash_read", map[string]any{"command": "go test ./... && go env GOCACHE GOMODCACHE"}),
	)

	send_agent_turn(harness)

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
