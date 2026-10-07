package integration_test

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestRead_CleanOpenBufferReturnsItsTextAndRecordsTheTick(t *testing.T) {
	harness, provider := start_with_vimtool(t, call("tc_1", "read", map[string]any{"path": "a.txt"}))
	path := filepath.Join(harness.Dir, "a.txt")
	harness.WriteFile("a.txt", "one\ntwo\n")
	harness.Command("edit " + path)

	send_agent_turn(harness)

	results := tool_results(t, provider)
	if got := result_text(results[0]); got != "1\tone\n2\ttwo\n3\t\n" || results[0].IsError {
		t.Fatalf("read result: %q", got)
	}
	if got, want := agent_tick(harness, path), buffer_tick(harness, path); got != want {
		t.Fatalf("recorded tick %d, buffer tick %d", got, want)
	}
}

func TestRead_BufferWithMyUnsavedChangesReturnsTheDisk(t *testing.T) {
	harness, provider := start_with_vimtool(t, call("tc_1", "read", map[string]any{"path": "a.txt"}))
	path := filepath.Join(harness.Dir, "a.txt")
	harness.WriteFile("a.txt", "one\n")
	harness.Command("edit " + path)
	harness.SetText("half finished\n")

	send_agent_turn(harness)

	results := tool_results(t, provider)
	if got := result_text(results[0]); got != "1\tone\n2\t\n" {
		t.Fatalf("read result: %q", got)
	}
	if got := buffer_lines(harness, path); got != "half finished\n" {
		t.Fatalf("buffer: %q", got)
	}
	if got, want := agent_tick(harness, path), buffer_tick(harness, path); got != want {
		t.Fatalf("recorded tick %d, buffer tick %d", got, want)
	}
}

func TestRead_FileThatIsNotOpenReturnsTheDisk(t *testing.T) {
	harness, provider := start_with_vimtool(t,
		call("tc_1", "read", map[string]any{"path": "a.txt"}),
		call("tc_2", "read", map[string]any{"path": "missing.txt"}))
	harness.WriteFile("a.txt", "one\n")

	send_agent_turn(harness)

	results := tool_results(t, provider)
	if got := result_text(results[0]); got != "1\tone\n2\t\n" {
		t.Fatalf("read result: %q", got)
	}
	if !results[1].IsError || !strings.Contains(result_text(results[1]), "failed to read file") {
		t.Fatalf("missing file: %#v", results[1])
	}
}
