package tool

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGrep_BasicMatch(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "test.txt"), []byte("hello world\ngoodbye world"), 0644)

	result, error := GrepTool().Execute(context.Background(), "", map[string]interface{}{
		"pattern": "hello", "path": dir,
	})

	if error != nil {
		t.Fatalf("unexpected error: %v", error)
	}
	text := extract_text(t, result)
	if !strings.Contains(text, "hello world") {
		t.Errorf("expected match in output, got: %s", text)
	}
}

func TestGrep_NoMatches(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "test.txt"), []byte("hello"), 0644)

	result, error := GrepTool().Execute(context.Background(), "", map[string]interface{}{
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

func TestGrep_IgnoreCase(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "test.txt"), []byte("Hello World"), 0644)

	result, error := GrepTool().Execute(context.Background(), "", map[string]interface{}{
		"pattern": "hello", "path": dir, "ignore_case": true,
	})

	if error != nil {
		t.Fatalf("unexpected error: %v", error)
	}
	text := extract_text(t, result)
	if !strings.Contains(text, "Hello World") {
		t.Errorf("expected case-insensitive match, got: %s", text)
	}
}

func TestGrep_GlobFilter(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "test.go"), []byte("match_target"), 0644)
	os.WriteFile(filepath.Join(dir, "test.txt"), []byte("match_target"), 0644)

	result, error := GrepTool().Execute(context.Background(), "", map[string]interface{}{
		"pattern": "match_target", "path": dir, "glob": "*.go",
	})

	if error != nil {
		t.Fatalf("unexpected error: %v", error)
	}
	text := extract_text(t, result)
	if !strings.Contains(text, "test.go") {
		t.Error("expected test.go in results")
	}
	if strings.Contains(text, "test.txt") {
		t.Error("test.txt should be filtered out by glob")
	}
}

func TestGrep_RegexPattern(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "test.txt"), []byte("foo123bar"), 0644)

	result, error := GrepTool().Execute(context.Background(), "", map[string]interface{}{
		"pattern": "foo\\d+bar", "path": dir,
	})

	if error != nil {
		t.Fatalf("unexpected error: %v", error)
	}
	text := extract_text(t, result)
	if !strings.Contains(text, "foo123bar") {
		t.Errorf("expected regex match, got: %s", text)
	}
}
