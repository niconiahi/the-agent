package integration_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/niconiahi/the-agent/nvim/nvimtest"
)

func TestEdit_ChangesTheOpenBufferSavesItAndOneUndoRevertsIt(t *testing.T) {
	harness, provider := start_with_vimtool(t, call("tc_1", "edit", map[string]any{"path": "a.txt", "old_text": "two\nthree", "new_text": "TWO\nTHREE\nFOUR"}))
	path := filepath.Join(harness.Dir, "a.txt")
	harness.WriteFile("a.txt", "one\ntwo\nthree\n")
	harness.Command("edit " + path)
	harness.Command("normal! ggAx")
	harness.Command("write")

	send_agent_turn(harness)

	results := tool_results(t, provider)
	if results[0].IsError || result_text(results[0]) != "Edited a.txt\n\n- two\n- three\n+ TWO\n+ THREE\n+ FOUR\n" {
		t.Fatalf("edit result: %#v", results[0])
	}
	if got := buffer_lines(harness, path); got != "onex\nTWO\nTHREE\nFOUR\n" {
		t.Fatalf("buffer: %q", got)
	}
	if got := harness.ReadFile("a.txt"); got != "onex\nTWO\nTHREE\nFOUR\n" {
		t.Fatalf("disk: %q", got)
	}
	if got, want := agent_tick(harness, path), buffer_tick(harness, path); got != want {
		t.Fatalf("recorded tick %d, buffer tick %d", got, want)
	}

	undo(harness, path)
	if got := buffer_lines(harness, path); got != "onex\ntwo\nthree\n" {
		t.Fatalf("after one undo: %q", got)
	}
}

func TestEdit_UnmatchedOrRepeatedOldTextIsAToolErrorAndChangesNothing(t *testing.T) {
	harness, provider := start_with_vimtool(t,
		call("tc_1", "edit", map[string]any{"path": "a.txt", "old_text": "nowhere", "new_text": "x"}),
		call("tc_2", "edit", map[string]any{"path": "a.txt", "old_text": "same", "new_text": "x"}))
	path := filepath.Join(harness.Dir, "a.txt")
	harness.WriteFile("a.txt", "same\nsame\n")
	harness.Command("edit " + path)
	before := buffer_tick(harness, path)

	send_agent_turn(harness)

	results := tool_results(t, provider)
	if !results[0].IsError || result_text(results[0]) != "old_text not found in file" {
		t.Fatalf("unmatched: %#v", results[0])
	}
	if !results[1].IsError || result_text(results[1]) != "old_text found 2 times, must be unique" {
		t.Fatalf("repeated: %#v", results[1])
	}
	if got := buffer_lines(harness, path); got != "same\nsame\n" {
		t.Fatalf("buffer: %q", got)
	}
	if got := harness.ReadFile("a.txt"); got != "same\nsame\n" {
		t.Fatalf("disk: %q", got)
	}
	if got := buffer_tick(harness, path); got != before {
		t.Fatalf("changedtick moved from %d to %d", before, got)
	}
}

func TestEdit_SetsMyUnsavedChangesAsideInASidecarThenEditsTheDiskVersion(t *testing.T) {
	config := nvimtest.Config()
	config.Now = fixed_clock("2026-10-06T14:32:00Z")
	harness, provider := start_with_vimtool_config(t, config, call("tc_1", "edit", map[string]any{"path": "a.txt", "old_text": "one", "new_text": "ONE"}))
	path := filepath.Join(harness.Dir, "a.txt")
	harness.WriteFile("a.txt", "one\n")
	harness.Command("edit " + path)
	harness.SetText("one mine\nand more")
	notifications := record_notifications(harness)

	send_agent_turn(harness)

	if results := tool_results(t, provider); results[0].IsError {
		t.Fatalf("edit result: %#v", results[0])
	}
	sidecar := ".the-agent/sessions/foo/unsaved/a.txt.2026-10-06T14:32:00Z"
	if got := harness.ReadFile(sidecar); got != "one mine\nand more\n" {
		t.Fatalf("sidecar: %q", got)
	}
	if got := buffer_lines(harness, path); got != "ONE\n" {
		t.Fatalf("buffer: %q", got)
	}
	if got := harness.ReadFile("a.txt"); got != "ONE\n" {
		t.Fatalf("disk: %q", got)
	}
	notes := notifications()
	if len(notes) != 1 || !strings.Contains(notes[0], filepath.Join(harness.Dir, sidecar)) {
		t.Fatalf("notifications: %q", notes)
	}
	undo(harness, path)
	if got := buffer_lines(harness, path); got != "one\n" {
		t.Fatalf("after one undo: %q", got)
	}
}

func TestEdit_CleanBufferPicksUpWhatChangedOnDiskFirst(t *testing.T) {
	harness, provider := start_with_vimtool(t, call("tc_1", "edit", map[string]any{"path": "a.txt", "old_text": "two", "new_text": "TWO"}))
	path := filepath.Join(harness.Dir, "a.txt")
	harness.WriteFile("a.txt", "one\n")
	harness.Command("edit " + path)
	harness.WriteFile("a.txt", "one\ntwo\n")

	send_agent_turn(harness)

	if results := tool_results(t, provider); results[0].IsError {
		t.Fatalf("edit result: %#v", results[0])
	}
	if got := harness.ReadFile("a.txt"); got != "one\nTWO\n" {
		t.Fatalf("disk: %q", got)
	}
}

func TestEdit_FileThatIsNotOpenIsLoadedIntoABufferEditedAndSaved(t *testing.T) {
	harness, provider := start_with_vimtool(t, call("tc_1", "edit", map[string]any{"path": "src/b.txt", "old_text": "beta\n", "new_text": "BETA\n"}))
	path := filepath.Join(harness.Dir, "src", "b.txt")
	harness.WriteFile("src/b.txt", "alpha\nbeta\n")

	send_agent_turn(harness)

	if results := tool_results(t, provider); results[0].IsError {
		t.Fatalf("edit result: %#v", results[0])
	}
	if got := harness.ReadFile("src/b.txt"); got != "alpha\nBETA\n" {
		t.Fatalf("disk: %q", got)
	}
	if got := buffer_lines(harness, path); got != "alpha\nBETA\n" {
		t.Fatalf("buffer: %q", got)
	}
	undo(harness, path)
	if got := buffer_lines(harness, path); got != "alpha\nbeta\n" {
		t.Fatalf("after one undo: %q", got)
	}
}
