package integration_test

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFilter_SortsTheBufferSavesItAndOneUndoRevertsIt(t *testing.T) {
	harness, provider := start_with_vimtool(t, call("tc_1", "filter", map[string]any{"path": "a.txt", "command": "sort"}))
	path := filepath.Join(harness.Dir, "a.txt")
	harness.WriteFile("a.txt", "cherry\napple\nbanana\n")
	harness.Command("edit " + path)

	send_agent_turn(harness)

	results := tool_results(t, provider)
	if results[0].IsError || result_text(results[0]) != "Filtered a.txt through sort" {
		t.Fatalf("filter result: %#v", results[0])
	}
	if got := buffer_lines(harness, path); got != "apple\nbanana\ncherry\n" {
		t.Fatalf("buffer: %q", got)
	}
	if got := harness.ReadFile("a.txt"); got != "apple\nbanana\ncherry\n" {
		t.Fatalf("disk: %q", got)
	}
	if got, want := agent_tick(harness, path), buffer_tick(harness, path); got != want {
		t.Fatalf("recorded tick %d, buffer tick %d", got, want)
	}
	undo(harness, path)
	if got := buffer_lines(harness, path); got != "cherry\napple\nbanana\n" {
		t.Fatalf("after one undo: %q", got)
	}
}

func TestFilter_GofmtFormatsAGoFileThatIsNotOpen(t *testing.T) {
	if _, error := exec.LookPath("gofmt"); error != nil {
		t.Skip("gofmt is not on PATH")
	}
	harness, provider := start_with_vimtool(t, call("tc_1", "filter", map[string]any{"path": "main.go", "command": "gofmt"}))
	path := filepath.Join(harness.Dir, "main.go")
	harness.WriteFile("main.go", "package main\nfunc main(){\nx:=1\n_ = x}\n")

	send_agent_turn(harness)

	if results := tool_results(t, provider); results[0].IsError {
		t.Fatalf("filter result: %#v", results[0])
	}
	if got := harness.ReadFile("main.go"); got != "package main\n\nfunc main() {\n\tx := 1\n\t_ = x\n}\n" {
		t.Fatalf("disk: %q", got)
	}
	undo(harness, path)
	if got := buffer_lines(harness, path); got != "package main\nfunc main(){\nx:=1\n_ = x}\n" {
		t.Fatalf("after one undo: %q", got)
	}
}

func TestFilter_FailingCommandIsAToolErrorAndChangesNothing(t *testing.T) {
	harness, provider := start_with_vimtool(t, call("tc_1", "filter", map[string]any{"path": "a.txt", "command": "echo broken; exit 3"}))
	path := filepath.Join(harness.Dir, "a.txt")
	harness.WriteFile("a.txt", "keep\nme\n")
	harness.Command("edit " + path)

	send_agent_turn(harness)

	results := tool_results(t, provider)
	if !results[0].IsError || !strings.Contains(result_text(results[0]), "exited with 3") || !strings.Contains(result_text(results[0]), "broken") {
		t.Fatalf("filter result: %#v", results[0])
	}
	if got := buffer_lines(harness, path); got != "keep\nme\n" {
		t.Fatalf("buffer: %q", got)
	}
	if got := harness.ReadFile("a.txt"); got != "keep\nme\n" {
		t.Fatalf("disk: %q", got)
	}
	undo(harness, path)
	if got := buffer_lines(harness, path); got != "keep\nme\n" {
		t.Fatalf("an undo step was left behind: %q", got)
	}
}
