package integration_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	neovim "github.com/neovim/go-client/nvim"

	"github.com/niconiahi/the-agent/layout"
	"github.com/niconiahi/the-agent/message"
	"github.com/niconiahi/the-agent/nvim"
	"github.com/niconiahi/the-agent/nvim/nvimtest"
	"github.com/niconiahi/the-agent/tool"
	"github.com/niconiahi/the-agent/vimtool"
)

func run_locally(invocation_context context.Context, directory string, script string) *exec.Cmd {
	command := exec.CommandContext(invocation_context, "/bin/bash", "-c", script)
	command.Dir = directory
	return command
}

var local_build struct {
	once   sync.Once
	binary string
	error  error
}

func local_binary(t *testing.T) string {
	t.Helper()
	local_build.once.Do(func() {
		directory, error := os.MkdirTemp("", "the-agent-binary-")
		if error != nil {
			local_build.error = error
			return
		}
		local_build.binary = filepath.Join(directory, "the-agent")
		build := exec.Command("go", "build", "-o", local_build.binary, "./cmd/agent")
		build.Dir = nvimtest.RepoRoot()
		if output, error := build.CombinedOutput(); error != nil {
			local_build.error = fmt.Errorf("go build: %v\n%s", error, output)
		}
	})
	if local_build.error != nil {
		t.Fatal(local_build.error)
	}
	return local_build.binary
}

func start_with_local_bash_write(t *testing.T, config nvim.Config, calls ...message.ToolCall) (*nvimtest.Harness, *nvimtest.Provider) {
	t.Helper()
	binary := local_binary(t)
	replies := []nvimtest.Reply{}
	for _, call := range calls {
		replies = append(replies, nvimtest.Reply{ToolCalls: []message.ToolCall{call}})
	}
	replies = append(replies, nvimtest.Text("done", 10))
	provider := nvimtest.RegisterProvider(t, replies...)
	harness := nvimtest.StartWithTools(t, config, func(client *neovim.Nvim) []tool.Tool {
		project := config.Project
		if project == "" {
			var directory string
			if error := client.Call("getcwd", &directory); error != nil {
				t.Fatal(error)
			}
			project = directory
		}
		clone := tool.Clone{Project: project, Binary: binary, Run: run_locally}
		if error := os.MkdirAll(layout.Clone(project), 0o700); error != nil {
			t.Fatal(error)
		}
		sandbox := tool.Sandbox{User: "_the-agent", Home: t.TempDir(), Project: project}
		return []tool.Tool{tool.BashWriteTool(sandbox, clone, vimtool.Replay(client))}
	})
	return harness, provider
}

func TestBashWrite_ChangeArrivesInTheBufferSavedAndOneUndoRevertsIt(t *testing.T) {
	harness, provider := start_with_local_bash_write(t, nvimtest.Config(), call("tc_1", "bash_write", map[string]any{"command": "echo two >> a.txt"}))
	path := filepath.Join(harness.Dir, "a.txt")
	harness.WriteFile("a.txt", "one\n")
	harness.Command("edit " + path)

	send_agent_turn(harness)

	results := tool_results(t, provider)
	if results[0].IsError || !strings.HasSuffix(result_text(results[0]), "Applied to the project:\nM a.txt") {
		t.Fatalf("bash_write result: %#v", results[0])
	}
	if got := buffer_lines(harness, path); got != "one\ntwo\n" {
		t.Fatalf("buffer: %q", got)
	}
	if got := harness.ReadFile("a.txt"); got != "one\ntwo\n" {
		t.Fatalf("disk: %q", got)
	}
	undo(harness, path)
	if got := buffer_lines(harness, path); got != "one\n" {
		t.Fatalf("after one undo: %q", got)
	}
}

func TestBashWrite_CreatesNewFilesAndDeletesRemovedOnes(t *testing.T) {
	harness, provider := start_with_local_bash_write(t, nvimtest.Config(), call("tc_1", "bash_write", map[string]any{"command": "rm old.txt && mkdir -p gen && echo generated > gen/new.txt"}))
	old := filepath.Join(harness.Dir, "old.txt")
	harness.WriteFile("old.txt", "old\n")
	harness.Command("edit " + old)

	send_agent_turn(harness)

	if results := tool_results(t, provider); results[0].IsError {
		t.Fatalf("bash_write result: %#v", results[0])
	}
	if got := harness.ReadFile("gen/new.txt"); got != "generated\n" {
		t.Fatalf("gen/new.txt: %q", got)
	}
	if _, error := os.Stat(old); !errors.Is(error, os.ErrNotExist) {
		t.Fatalf("old.txt still exists: %v", error)
	}
	var loaded bool
	if error := harness.Nvim.ExecLua(`local b = vim.fn.bufnr(...); return b ~= -1 and vim.api.nvim_buf_is_loaded(b)`, &loaded, old); error != nil {
		t.Fatal(error)
	}
	if loaded {
		t.Fatal("the deleted file's buffer is still loaded")
	}
}

func TestBashWrite_SetsMyUnsavedChangesAsideBeforeTheReplay(t *testing.T) {
	config := nvimtest.Config()
	config.Now = fixed_clock("2026-10-06T14:32:00Z")
	harness, provider := start_with_local_bash_write(t, config,
		call("tc_1", "bash_write", map[string]any{"command": "echo theirs > b.txt && rm c.txt"}))
	changed := filepath.Join(harness.Dir, "b.txt")
	deleted := filepath.Join(harness.Dir, "c.txt")
	harness.WriteFile("b.txt", "saved\n")
	harness.WriteFile("c.txt", "saved\n")
	harness.Command("edit " + deleted)
	harness.SetText("mine in c\n")
	harness.Command("edit " + changed)
	harness.SetText("mine in b\n")
	notifications := record_notifications(harness)

	send_agent_turn(harness)

	if results := tool_results(t, provider); results[0].IsError {
		t.Fatalf("bash_write result: %#v", results[0])
	}
	if got := harness.ReadFile(".the-agent/sessions/foo/unsaved/b.txt.2026-10-06T14:32:00Z"); got != "mine in b\n" {
		t.Fatalf("b sidecar: %q", got)
	}
	if got := harness.ReadFile(".the-agent/sessions/foo/unsaved/c.txt.2026-10-06T14:32:00Z"); got != "mine in c\n" {
		t.Fatalf("c sidecar: %q", got)
	}
	if got := buffer_lines(harness, changed); got != "theirs\n" {
		t.Fatalf("buffer: %q", got)
	}
	if notes := notifications(); len(notes) != 2 {
		t.Fatalf("want one notification per sidecar, got %q", notes)
	}
}
