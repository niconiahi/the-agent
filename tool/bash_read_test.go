package tool

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/niconiahi/the-agent/setup"
)

func test_project(t *testing.T) string {
	t.Helper()
	directory, error := os.Getwd()
	if error != nil {
		t.Fatal(error)
	}
	project, error := filepath.EvalSymlinks(filepath.Dir(directory))
	if error != nil {
		t.Fatal(error)
	}
	return project
}

func require_sandbox(t *testing.T) Sandbox {
	t.Helper()
	project := test_project(t)
	if output, error := exec.Command("sudo", "-n", "-u", setup.USER, "test", "-r", project).CombinedOutput(); error != nil {
		t.Skipf("%s cannot run commands in %s (%s): run sudo the-agent setup to enable this test", setup.USER, project, strings.TrimSpace(string(output)))
	}
	return Sandbox{User: setup.USER, Home: setup.Home(), Project: project}
}

func TestBashRead_WithoutSetupFailsAtOnceWithTheSetupCommand(t *testing.T) {
	sandbox := Sandbox{User: "_the-agent-missing", Home: setup.Home(), Project: test_project(t)}
	start := time.Now()

	_, error := BashReadTool(sandbox).Execute(context.Background(), "", map[string]interface{}{
		"command": "true",
	})

	if error == nil || !strings.Contains(error.Error(), "sudo the-agent setup") {
		t.Fatalf("want an error naming sudo the-agent setup, got %v", error)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("sudo -n must fail at once, took %v", elapsed)
	}
}

func TestBashRead_TmpdirIsTheProjectsTheAgentTmp(t *testing.T) {
	sandbox := Sandbox{User: setup.USER, Home: "/var/the-agent", Project: "/work/app"}

	environment := sandbox.Environment()

	for _, want := range []string{"TMPDIR=/work/app/.the-agent/tmp", "GOCACHE=/var/the-agent/gocache", "GOMODCACHE=/var/the-agent/gomodcache"} {
		found := false
		for _, variable := range environment {
			found = found || variable == want
		}
		if !found {
			t.Errorf("want %s in %v", want, environment)
		}
	}
}

func TestBashRead_SimpleCommand(t *testing.T) {
	result, error := BashReadTool(require_sandbox(t)).Execute(context.Background(), "", map[string]interface{}{
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

func TestBashRead_ExitCodeNonZero(t *testing.T) {
	result, error := BashReadTool(require_sandbox(t)).Execute(context.Background(), "", map[string]interface{}{
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

func TestBashRead_StderrCapture(t *testing.T) {
	result, error := BashReadTool(require_sandbox(t)).Execute(context.Background(), "", map[string]interface{}{
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

func TestBashRead_StdoutAndStderr(t *testing.T) {
	result, error := BashReadTool(require_sandbox(t)).Execute(context.Background(), "", map[string]interface{}{
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

func TestBashRead_Timeout(t *testing.T) {
	start := time.Now()
	_, error := BashReadTool(require_sandbox(t)).Execute(context.Background(), "", map[string]interface{}{
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

func TestBashRead_ContextCancellation(t *testing.T) {
	cancelled_context, cancel := context.WithCancel(context.Background())
	cancel()

	_, error := BashReadTool(require_sandbox(t)).Execute(cancelled_context, "", map[string]interface{}{
		"command": "sleep 60",
	})

	if error == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestBashRead_OutputTruncation(t *testing.T) {
	result, error := BashReadTool(require_sandbox(t)).Execute(context.Background(), "", map[string]interface{}{
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
