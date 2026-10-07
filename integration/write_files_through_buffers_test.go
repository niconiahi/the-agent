package integration_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/niconiahi/the-agent/nvim/nvimtest"
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

func TestWrite_SetsMyUnsavedChangesAsideInASidecarFirst(t *testing.T) {
	config := nvimtest.Config()
	config.Now = fixed_clock("2026-10-06T14:32:00Z")
	harness, provider := start_with_vimtool_config(t, config, call("tc_1", "write", map[string]any{"path": "src/b.txt", "content": "theirs\n"}))
	path := filepath.Join(harness.Dir, "src", "b.txt")
	harness.WriteFile("src/b.txt", "saved\n")
	harness.Command("edit " + path)
	harness.SetText("mine\n")
	notifications := record_notifications(harness)

	send_agent_turn(harness)

	if results := tool_results(t, provider); results[0].IsError {
		t.Fatalf("write result: %#v", results[0])
	}
	sidecar := ".the-agent/sessions/foo/unsaved/src/b.txt.2026-10-06T14:32:00Z"
	if got := harness.ReadFile(sidecar); got != "mine\n" {
		t.Fatalf("sidecar: %q", got)
	}
	if got := buffer_lines(harness, path); got != "theirs\n" {
		t.Fatalf("buffer: %q", got)
	}
	if got := harness.ReadFile("src/b.txt"); got != "theirs\n" {
		t.Fatalf("disk: %q", got)
	}
	notes := notifications()
	if len(notes) != 1 || !strings.Contains(notes[0], filepath.Join(harness.Dir, sidecar)) {
		t.Fatalf("notifications: %q", notes)
	}
}
