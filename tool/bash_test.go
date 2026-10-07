package tool

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestBash_SimpleCommand(t *testing.T) {
	result, error := BashTool().Execute(context.Background(), "", map[string]interface{}{
		"command": "echo hello world",
	})

	if error != nil {
		t.Fatalf("unexpected error: %v", error)
	}
	text := extract_text(t, result)
	if strings.TrimSpace(text) != "hello world" {
		t.Errorf("expected 'hello world', got '%s'", strings.TrimSpace(text))
	}
	details, ok := result.Details.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map details, got %T", result.Details)
	}
	if details["exit_code"] != 0 {
		t.Errorf("expected exit code 0, got %v", details["exit_code"])
	}
}

func TestBash_ExitCodeNonZero(t *testing.T) {
	result, error := BashTool().Execute(context.Background(), "", map[string]interface{}{
		"command": "exit 42",
	})

	if error != nil {
		t.Fatal("exit code 42 should NOT return an error — exit codes are results, not failures")
	}
	details, ok := result.Details.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map details, got %T", result.Details)
	}
	if details["exit_code"] != 42 {
		t.Errorf("expected exit code 42, got %v", details["exit_code"])
	}
}

func TestBash_StderrCapture(t *testing.T) {
	result, error := BashTool().Execute(context.Background(), "", map[string]interface{}{
		"command": "echo error_output >&2",
	})

	if error != nil {
		t.Fatalf("unexpected error: %v", error)
	}
	text := extract_text(t, result)
	if !strings.Contains(text, "error_output") {
		t.Errorf("expected stderr in output, got: '%s'", text)
	}
}

func TestBash_StdoutAndStderr(t *testing.T) {
	result, error := BashTool().Execute(context.Background(), "", map[string]interface{}{
		"command": "echo stdout_line && echo stderr_line >&2",
	})

	if error != nil {
		t.Fatalf("unexpected error: %v", error)
	}
	text := extract_text(t, result)
	if !strings.Contains(text, "stdout_line") {
		t.Error("expected stdout in output")
	}
	if !strings.Contains(text, "stderr_line") {
		t.Error("expected stderr in output")
	}
}

func TestBash_Timeout(t *testing.T) {
	start := time.Now()
	_, error := BashTool().Execute(context.Background(), "", map[string]interface{}{
		"command": "sleep 60",
		"timeout": float64(1),
	})

	elapsed := time.Since(start)
	if error != nil {
		t.Fatalf("unexpected error: %v", error)
	}
	if elapsed > 5*time.Second {
		t.Errorf("timeout should have killed the command quickly, took %v", elapsed)
	}
}

func TestBash_ContextCancellation(t *testing.T) {
	cancelled_context, cancel := context.WithCancel(context.Background())
	cancel()

	_, error := BashTool().Execute(cancelled_context, "", map[string]interface{}{
		"command": "sleep 60",
	})

	if error == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestBash_OutputTruncation(t *testing.T) {
	result, error := BashTool().Execute(context.Background(), "", map[string]interface{}{
		"command": "yes | head -200000",
	})

	if error != nil {
		t.Fatalf("unexpected error: %v", error)
	}
	text := extract_text(t, result)
	if !strings.Contains(text, "(output truncated)") {
		t.Error("expected truncation message for large output")
	}
	if len(text) > MAX_OUTPUT_BYTES+100 {
		t.Errorf("output should be around %d bytes, got %d", MAX_OUTPUT_BYTES, len(text))
	}
}
