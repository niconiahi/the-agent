package tool

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestWrite_NewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new.txt")

	_, error := WriteTool().Execute(context.Background(), "", map[string]interface{}{
		"path": path, "content": "hello",
	})

	if error != nil {
		t.Fatalf("unexpected error: %v", error)
	}

	content, _ := os.ReadFile(path)
	if string(content) != "hello" {
		t.Errorf("expected 'hello', got '%s'", string(content))
	}
}

func TestWrite_OverwriteExisting(t *testing.T) {
	path := write_temp_file(t, "old content")

	_, error := WriteTool().Execute(context.Background(), "", map[string]interface{}{
		"path": path, "content": "new content",
	})

	if error != nil {
		t.Fatalf("unexpected error: %v", error)
	}

	content, _ := os.ReadFile(path)
	if string(content) != "new content" {
		t.Errorf("expected 'new content', got '%s'", string(content))
	}
}

func TestWrite_CreatesParentDirectories(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a", "b", "c", "file.txt")

	_, error := WriteTool().Execute(context.Background(), "", map[string]interface{}{
		"path": path, "content": "deep",
	})

	if error != nil {
		t.Fatalf("unexpected error: %v", error)
	}

	content, _ := os.ReadFile(path)
	if string(content) != "deep" {
		t.Errorf("expected 'deep', got '%s'", string(content))
	}
}

func TestWrite_EmptyContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.txt")

	_, error := WriteTool().Execute(context.Background(), "", map[string]interface{}{
		"path": path, "content": "",
	})

	if error != nil {
		t.Fatalf("unexpected error: %v", error)
	}

	content, _ := os.ReadFile(path)
	if len(content) != 0 {
		t.Errorf("expected empty file, got %d bytes", len(content))
	}
}
