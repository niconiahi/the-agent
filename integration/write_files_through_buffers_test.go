package integration_test

import (
	"path/filepath"
	"testing"
)

func TestWrite_CreatesANewFileThroughABufferAndSavesIt(t *testing.T) {
	harness, provider := start_with_vimtool(t, call("tc_1", "write", map[string]any{"path": "src/new.txt", "content": "alpha\nbeta\n"}))
	path := filepath.Join(harness.Dir, "src", "new.txt")

	send_agent_turn(harness)

	results := tool_results(t, provider)
	if results[0].IsError || result_text(results[0]) != "Wrote src/new.txt" {
		t.Fatalf("write result: %#v", results[0])
	}
	if got := harness.ReadFile("src/new.txt"); got != "alpha\nbeta\n" {
		t.Fatalf("disk: %q", got)
	}
	if got := buffer_lines(harness, path); got != "alpha\nbeta\n" {
		t.Fatalf("buffer: %q", got)
	}
}

func TestWrite_ReplacesAnOpenBufferSavesItAndOneUndoRevertsIt(t *testing.T) {
	harness, provider := start_with_vimtool(t, call("tc_1", "write", map[string]any{"path": "a.txt", "content": "new\ncontent\n"}))
	path := filepath.Join(harness.Dir, "a.txt")
	harness.WriteFile("a.txt", "old\n")
	harness.Command("edit " + path)

	send_agent_turn(harness)

	if results := tool_results(t, provider); results[0].IsError {
		t.Fatalf("write result: %#v", results[0])
	}
	if got := buffer_lines(harness, path); got != "new\ncontent\n" {
		t.Fatalf("buffer: %q", got)
	}
	if got := harness.ReadFile("a.txt"); got != "new\ncontent\n" {
		t.Fatalf("disk: %q", got)
	}
	if got, want := agent_tick(harness, path), buffer_tick(harness, path); got != want {
		t.Fatalf("recorded tick %d, buffer tick %d", got, want)
	}
	undo(harness, path)
	if got := buffer_lines(harness, path); got != "old\n" {
		t.Fatalf("after one undo: %q", got)
	}
}
