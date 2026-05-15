package tool

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLs_BasicDirectory(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte(""), 0644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte(""), 0644)

	result, error := LsTool().Execute(context.Background(), "", map[string]interface{}{"path": dir})

	if error != nil {
		t.Fatalf("unexpected error: %v", error)
	}
	text := extract_text(t, result)
	if !strings.Contains(text, "a.txt") || !strings.Contains(text, "b.txt") {
		t.Errorf("expected a.txt and b.txt, got: %s", text)
	}
}

func TestLs_DirectoriesMarked(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "file.txt"), []byte(""), 0644)
	os.Mkdir(filepath.Join(dir, "subdir"), 0755)

	result, error := LsTool().Execute(context.Background(), "", map[string]interface{}{"path": dir})

	if error != nil {
		t.Fatalf("unexpected error: %v", error)
	}
	text := extract_text(t, result)
	if !strings.Contains(text, "subdir/") {
		t.Error("expected directory to have trailing /")
	}
	if strings.Contains(text, "file.txt/") {
		t.Error("file should not have trailing /")
	}
}

func TestLs_AlphabeticalSort(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "z.txt"), []byte(""), 0644)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte(""), 0644)
	os.WriteFile(filepath.Join(dir, "m.txt"), []byte(""), 0644)

	result, error := LsTool().Execute(context.Background(), "", map[string]interface{}{"path": dir})

	if error != nil {
		t.Fatalf("unexpected error: %v", error)
	}
	text := extract_text(t, result)
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
	if lines[0] != "a.txt" || lines[1] != "m.txt" || lines[2] != "z.txt" {
		t.Errorf("expected alphabetical order, got: %v", lines)
	}
}

func TestLs_LimitTruncation(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 10; i++ {
		os.WriteFile(filepath.Join(dir, strings.Repeat("a", i+1)+".txt"), []byte(""), 0644)
	}

	result, error := LsTool().Execute(context.Background(), "", map[string]interface{}{
		"path": dir, "limit": float64(3),
	})

	if error != nil {
		t.Fatalf("unexpected error: %v", error)
	}
	text := extract_text(t, result)
	if !strings.Contains(text, "truncated") {
		t.Error("expected truncation message")
	}
}

func TestLs_EmptyDirectory(t *testing.T) {
	dir := t.TempDir()

	result, error := LsTool().Execute(context.Background(), "", map[string]interface{}{"path": dir})

	if error != nil {
		t.Fatalf("unexpected error: %v", error)
	}
	text := extract_text(t, result)
	if strings.TrimSpace(text) != "" {
		t.Errorf("expected empty output for empty dir, got: '%s'", text)
	}
}

func TestLs_NonexistentDirectory(t *testing.T) {
	_, error := LsTool().Execute(context.Background(), "", map[string]interface{}{
		"path": "/tmp/nonexistent_ls_test_abc123",
	})

	if error == nil {
		t.Fatal("expected error for nonexistent directory")
	}
}
