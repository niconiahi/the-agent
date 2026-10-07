package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/niconiahi/the-agent/message"
)

const DEFAULT_BASH_TIMEOUT = 120 * time.Second
const BASH_STOP_GRACE = 5 * time.Second

var SUDO_REFUSALS = []string{"a password is required", "unknown user", "not allowed", "is not in the sudoers file"}

type Sandbox struct {
	User    string
	Home    string
	Project string
}

func (sandbox Sandbox) Environment() []string {
	return []string{
		"HOME=" + sandbox.Home,
		"PATH=" + os.Getenv("PATH"),
		"TMPDIR=" + filepath.Join(sandbox.Home, "tmp"),
		"GOCACHE=" + filepath.Join(sandbox.Home, "gocache"),
		"GOMODCACHE=" + filepath.Join(sandbox.Home, "gomodcache"),
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=safe.directory",
		"GIT_CONFIG_VALUE_0=*",
	}
}

func (sandbox Sandbox) Command(invocation_context context.Context, directory string, script string) *exec.Cmd {
	arguments := append([]string{"-n", "-u", sandbox.User, "/usr/bin/env"}, sandbox.Environment()...)
	arguments = append(arguments, "/bin/bash", "-c", script)
	command := exec.CommandContext(invocation_context, "sudo", arguments...)
	command.Dir = directory
	command.Cancel = func() error {
		return command.Process.Signal(syscall.SIGTERM)
	}
	command.WaitDelay = BASH_STOP_GRACE
	return command
}

func BashReadTool(sandbox Sandbox) Tool {
	parameters := json.RawMessage(`{
		"type": "object",
		"properties": {
			"command": {"type": "string", "description": "The bash command to run in the project"},
			"timeout": {"type": "integer", "description": "Timeout in seconds"}
		},
		"required": ["command"]
	}`)

	description := "Run a bash command in the project as the read-only user " + sandbox.User + ": tests, builds, git log, inspection. Writing the project fails with Permission denied; use bash_write for commands that must change files. TMPDIR and the Go caches are writable."
	return NewTool("bash_read", description, parameters, func(invocation_context context.Context, _ string, arguments map[string]interface{}) (ToolResult, error) {
		return run_bash(invocation_context, sandbox, func(command_context context.Context, script string) *exec.Cmd {
			return sandbox.Command(command_context, sandbox.Project, script)
		}, arguments)
	})
}

func run_bash(invocation_context context.Context, sandbox Sandbox, start func(context.Context, string) *exec.Cmd, arguments map[string]interface{}) (ToolResult, error) {
	script, ok := arguments["command"].(string)
	if !ok {
		return ToolResult{}, fmt.Errorf("command is required")
	}

	timeout := DEFAULT_BASH_TIMEOUT
	if raw_timeout, ok := arguments["timeout"].(float64); ok {
		timeout = time.Duration(raw_timeout) * time.Second
	}

	command_context, cancel := context.WithTimeout(invocation_context, timeout)
	defer cancel()

	command := start(command_context, script)

	var stdout_buffer bytes.Buffer
	var stderr_buffer bytes.Buffer
	command.Stdout = &stdout_buffer
	command.Stderr = &stderr_buffer

	error := command.Run()

	exit_code := 0
	if error != nil {
		exit_error, ok := error.(*exec.ExitError)
		if !ok {
			return ToolResult{}, fmt.Errorf("failed to run command: %v", error)
		}
		exit_code = exit_error.ExitCode()
	}

	stderr_output := stderr_buffer.String()
	if exit_code == 1 && stdout_buffer.Len() == 0 && sudo_refused(stderr_output) {
		return ToolResult{}, setup_error(sandbox, stderr_output)
	}

	output := stdout_buffer.String()
	if stderr_output != "" {
		if output != "" {
			output += "\n"
		}
		output += stderr_output
	}

	if len(output) > MAX_OUTPUT_BYTES {
		output = output[:MAX_OUTPUT_BYTES] + "\n(output truncated)"
	}

	return ToolResult{
		Content: []message.Content{message.TextContent{Text: output}},
		Details: map[string]interface{}{"exit_code": exit_code},
	}, nil
}

func setup_error(sandbox Sandbox, stderr string) error {
	return fmt.Errorf("cannot run commands as %s (%s): run sudo the-agent setup %s", sandbox.User, strings.TrimSpace(stderr), sandbox.Project)
}

func sudo_refused(stderr string) bool {
	if !strings.HasPrefix(stderr, "sudo: ") {
		return false
	}
	for _, refusal := range SUDO_REFUSALS {
		if strings.Contains(stderr, refusal) {
			return true
		}
	}
	return false
}
