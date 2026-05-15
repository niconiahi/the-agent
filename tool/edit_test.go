package tool

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestEdit_BasicReplace(t *testing.T) {
	path := write_temp_file(t, "hello world")

	result, error := EditTool().Execute(context.Background(), "", map[string]interface{}{
		"path": path, "old_text": "hello", "new_text": "goodbye",
	})

	if error != nil {
		t.Fatalf("unexpected error: %v", error)
	}

	content, _ := os.ReadFile(path)
	if string(content) != "goodbye world" {
		t.Errorf("expected 'goodbye world', got '%s'", string(content))
	}

	text := extract_text(t, result)
	if !strings.Contains(text, "- hello") || !strings.Contains(text, "+ goodbye") {
		t.Errorf("expected diff in result, got: %s", text)
	}
}

func TestEdit_MultilineReplace(t *testing.T) {
	path := write_temp_file(t, "line1\nline2\nline3")

	_, error := EditTool().Execute(context.Background(), "", map[string]interface{}{
		"path": path, "old_text": "line1\nline2", "new_text": "changed1\nchanged2",
	})

	if error != nil {
		t.Fatalf("unexpected error: %v", error)
	}

	content, _ := os.ReadFile(path)
	if string(content) != "changed1\nchanged2\nline3" {
		t.Errorf("expected 'changed1\\nchanged2\\nline3', got '%s'", string(content))
	}
}

func TestEdit_OldTextNotFound(t *testing.T) {
	path := write_temp_file(t, "hello")

	_, error := EditTool().Execute(context.Background(), "", map[string]interface{}{
		"path": path, "old_text": "nonexistent", "new_text": "whatever",
	})

	if error == nil {
		t.Fatal("expected error for old_text not found")
	}
	if !strings.Contains(error.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got: %s", error.Error())
	}
}

func TestEdit_OldTextMultipleMatches(t *testing.T) {
	path := write_temp_file(t, "aaa bbb aaa")

	_, error := EditTool().Execute(context.Background(), "", map[string]interface{}{
		"path": path, "old_text": "aaa", "new_text": "ccc",
	})

	if error == nil {
		t.Fatal("expected error for multiple matches")
	}
	if !strings.Contains(error.Error(), "2 times") {
		t.Errorf("expected '2 times' in error, got: %s", error.Error())
	}
}

func TestEdit_FileNotFound(t *testing.T) {
	_, error := EditTool().Execute(context.Background(), "", map[string]interface{}{
		"path": "/tmp/nonexistent_edit_test_abc123", "old_text": "x", "new_text": "y",
	})

	if error == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestEdit_ReplaceWithEmpty(t *testing.T) {
	path := write_temp_file(t, "keep remove keep")

	_, error := EditTool().Execute(context.Background(), "", map[string]interface{}{
		"path": path, "old_text": " remove", "new_text": "",
	})

	if error != nil {
		t.Fatalf("unexpected error: %v", error)
	}

	content, _ := os.ReadFile(path)
	if string(content) != "keep keep" {
		t.Errorf("expected 'keep keep', got '%s'", string(content))
	}
}

func TestGenerateDiff_SingleLine(t *testing.T) {
	diff := generate_diff("hello", "world")

	if diff != "- hello\n+ world\n" {
		t.Errorf("expected '- hello\\n+ world\\n', got '%s'", diff)
	}
}

func TestGenerateDiff_MultiLine(t *testing.T) {
	diff := generate_diff("line1\nline2", "line1\nchanged")

	if !strings.Contains(diff, "- line1") || !strings.Contains(diff, "- line2") {
		t.Error("expected old lines with - prefix")
	}
	if !strings.Contains(diff, "+ line1") || !strings.Contains(diff, "+ changed") {
		t.Error("expected new lines with + prefix")
	}
}

func TestGenerateDiff_AddLines(t *testing.T) {
	diff := generate_diff("one", "one\ntwo\nthree")

	plus_count := strings.Count(diff, "+ ")
	minus_count := strings.Count(diff, "- ")
	if minus_count != 1 {
		t.Errorf("expected 1 removed line, got %d", minus_count)
	}
	if plus_count != 3 {
		t.Errorf("expected 3 added lines, got %d", plus_count)
	}
}

func TestGenerateDiff_RemoveLines(t *testing.T) {
	diff := generate_diff("one\ntwo\nthree", "one")

	plus_count := strings.Count(diff, "+ ")
	minus_count := strings.Count(diff, "- ")
	if minus_count != 3 {
		t.Errorf("expected 3 removed lines, got %d", minus_count)
	}
	if plus_count != 1 {
		t.Errorf("expected 1 added line, got %d", plus_count)
	}
}
