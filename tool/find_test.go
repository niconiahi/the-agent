package tool

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func skip_without_fd(t *testing.T) {
	if _, error := exec.LookPath("fd"); error != nil {
		t.Skip("fd not installed, skipping find test")
	}
}

func TestFind_BasicPattern(t *testing.T) {
	skip_without_fd(t)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "hello.txt"), []byte(""), 0644)
	os.WriteFile(filepath.Join(dir, "world.go"), []byte(""), 0644)

	result, error := FindTool().Execute(context.Background(), "", map[string]interface{}{
		"pattern": "hello", "path": dir,
	})

	if error != nil {
		t.Fatalf("unexpected error: %v", error)
	}
	text := extract_text(t, result)
	if !strings.Contains(text, "hello.txt") {
		t.Errorf("expected hello.txt in results, got: %s", text)
	}
}

func TestFind_NoMatches(t *testing.T) {
	skip_without_fd(t)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "hello.txt"), []byte(""), 0644)

	result, error := FindTool().Execute(context.Background(), "", map[string]interface{}{
		"pattern": "nonexistent", "path": dir,
	})

	if error != nil {
		t.Fatalf("unexpected error: %v", error)
	}
	text := extract_text(t, result)
	if text != "No matches found" {
		t.Errorf("expected 'No matches found', got: '%s'", text)
	}
}

func TestFind_GlobPattern(t *testing.T) {
	skip_without_fd(t)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.go"), []byte(""), 0644)
	os.WriteFile(filepath.Join(dir, "b.go"), []byte(""), 0644)
	os.WriteFile(filepath.Join(dir, "c.txt"), []byte(""), 0644)

	result, error := FindTool().Execute(context.Background(), "", map[string]interface{}{
		"pattern": ".go", "path": dir,
	})

	if error != nil {
		t.Fatalf("unexpected error: %v", error)
	}
	text := extract_text(t, result)
	if !strings.Contains(text, "a.go") || !strings.Contains(text, "b.go") {
		t.Errorf("expected .go files, got: %s", text)
	}
}
