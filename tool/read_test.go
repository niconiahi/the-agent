package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/niconiahi/the-agent/message"
)

func TestRead_BasicFile(t *testing.T) {
	path := write_temp_file(t, "line1\nline2\nline3")

	result, error := ReadTool().Execute(context.Background(), "", map[string]interface{}{"path": path})

	if error != nil {
		t.Fatalf("unexpected error: %v", error)
	}
	text := extract_text(t, result)
	if !strings.Contains(text, "1\tline1") {
		t.Errorf("expected line number formatting, got: %s", text)
	}
	if !strings.Contains(text, "3\tline3") {
		t.Errorf("expected line 3, got: %s", text)
	}
}

func TestRead_WithOffset(t *testing.T) {
	lines := make([]string, 10)
	for i := range lines {
		lines[i] = fmt.Sprintf("line%d", i+1)
	}
	path := write_temp_file(t, strings.Join(lines, "\n"))

	result, error := ReadTool().Execute(context.Background(), "", map[string]interface{}{
		"path": path, "offset": float64(5),
	})

	if error != nil {
		t.Fatalf("unexpected error: %v", error)
	}
	text := extract_text(t, result)
	if !strings.HasPrefix(text, "5\tline5") {
		t.Errorf("expected to start at line 5, got: %s", text[:min(40, len(text))])
	}
}

func TestRead_WithLimit(t *testing.T) {
	lines := make([]string, 10)
	for i := range lines {
		lines[i] = fmt.Sprintf("line%d", i+1)
	}
	path := write_temp_file(t, strings.Join(lines, "\n"))

	result, error := ReadTool().Execute(context.Background(), "", map[string]interface{}{
		"path": path, "limit": float64(3),
	})

	if error != nil {
		t.Fatalf("unexpected error: %v", error)
	}
	text := extract_text(t, result)
	if strings.Contains(text, "4\tline4") {
		t.Error("should not contain line 4 with limit 3")
	}
	if !strings.Contains(text, "truncated") {
		t.Error("expected truncation message")
	}
}

func TestRead_OffsetAndLimit(t *testing.T) {
	lines := make([]string, 10)
	for i := range lines {
		lines[i] = fmt.Sprintf("line%d", i+1)
	}
	path := write_temp_file(t, strings.Join(lines, "\n"))

	result, error := ReadTool().Execute(context.Background(), "", map[string]interface{}{
		"path": path, "offset": float64(3), "limit": float64(2),
	})

	if error != nil {
		t.Fatalf("unexpected error: %v", error)
	}
	text := extract_text(t, result)
	if !strings.Contains(text, "3\tline3") {
		t.Error("expected line 3")
	}
	if !strings.Contains(text, "4\tline4") {
		t.Error("expected line 4")
	}
	if strings.Contains(text, "5\tline5") {
		t.Error("should not contain line 5")
	}
}

func TestRead_OffsetBeyondFileLength(t *testing.T) {
	path := write_temp_file(t, "one\ntwo\nthree")

	result, error := ReadTool().Execute(context.Background(), "", map[string]interface{}{
		"path": path, "offset": float64(100),
	})

	if error != nil {
		t.Fatalf("unexpected error: %v", error)
	}
	text := extract_text(t, result)
	if text != "" {
		t.Errorf("expected empty text, got: '%s'", text)
	}
}

func TestRead_OffsetLessThanOne(t *testing.T) {
	path := write_temp_file(t, "first\nsecond")

	result, error := ReadTool().Execute(context.Background(), "", map[string]interface{}{
		"path": path, "offset": float64(0),
	})

	if error != nil {
		t.Fatalf("unexpected error: %v", error)
	}
	text := extract_text(t, result)
	if !strings.HasPrefix(text, "1\tfirst") {
		t.Errorf("expected to start at line 1, got: %s", text[:min(30, len(text))])
	}
}

func TestRead_NonexistentFile(t *testing.T) {
	_, error := ReadTool().Execute(context.Background(), "", map[string]interface{}{
		"path": "/tmp/nonexistent_file_abc123_xyz789",
	})

	if error == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestRead_EmptyFile(t *testing.T) {
	path := write_temp_file(t, "")

	result, error := ReadTool().Execute(context.Background(), "", map[string]interface{}{"path": path})

	if error != nil {
		t.Fatalf("unexpected error: %v", error)
	}
	text := extract_text(t, result)
	if !strings.Contains(text, "1\t") {
		t.Errorf("expected one line with line number, got: '%s'", text)
	}
}

func TestRead_LargeFileTruncation(t *testing.T) {
	lines := make([]string, 6000)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i+1)
	}
	path := write_temp_file(t, strings.Join(lines, "\n"))

	result, error := ReadTool().Execute(context.Background(), "", map[string]interface{}{"path": path})

	if error != nil {
		t.Fatalf("unexpected error: %v", error)
	}
	text := extract_text(t, result)
	if !strings.Contains(text, "truncated") {
		t.Error("expected truncation message for 6000-line file")
	}
	if strings.Contains(text, "5001\t") {
		t.Error("should not contain line 5001")
	}
}

func write_temp_file(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	if os.WriteFile(path, []byte(content), 0644) != nil {
		t.Fatal("failed to write temp file")
	}
	return path
}

func extract_text(t *testing.T, result ToolResult) string {
	t.Helper()
	if len(result.Content) == 0 {
		return ""
	}
	tc, ok := result.Content[0].(message.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	return tc.Text
}
