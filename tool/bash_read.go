package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/niconiahi/the-agent/layout"
	"github.com/niconiahi/the-agent/message"
	"github.com/niconiahi/the-agent/setup"
)

const DEFAULT_BASH_TIMEOUT = 120 * time.Second
const BASH_STOP_GRACE = 5 * time.Second

type Sandbox struct {
	User    string
	Home    string
	Project string
}

func (sandbox Sandbox) Environment() []string {
	return []string{
		"HOME=" + sandbox.Home,
		"PATH=" + os.Getenv("PATH"),
		"TMPDIR=" + layout.Tmp(sandbox.Project),
		"GOCACHE=" + filepath.Join(sandbox.Home, layout.GO_CACHE),
		"GOMODCACHE=" + filepath.Join(sandbox.Home, layout.GO_MODULE_CACHE),
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

	result, error := capture(start(command_context, script), nil)
	if error != nil {
		return ToolResult{}, fmt.Errorf("failed to run command: %v", error)
	}

	if result.exit_code == 1 && len(result.stdout) == 0 && sudo_refused(result.stderr) {
		return ToolResult{}, setup_error(sandbox, result.stderr)
	}

	output := string(result.stdout)
	if result.stderr != "" {
		if output != "" {
			output += "\n"
		}
		output += result.stderr
	}

	if len(output) > MAX_OUTPUT_BYTES {
		output = output[:MAX_OUTPUT_BYTES] + "\n(output truncated)"
	}

	return ToolResult{
		Content: []message.Content{message.TextContent{Text: output}},
		Details: map[string]interface{}{"exit_code": result.exit_code},
	}, nil
}

func setup_error(sandbox Sandbox, stderr string) error {
	return fmt.Errorf("cannot run commands as %s (%s): run sudo the-agent setup %s", sandbox.User, strings.TrimSpace(stderr), sandbox.Project)
}

func sudo_refused(stderr string) bool {
	return strings.HasPrefix(stderr, "sudo: ") && setup.Refused(stderr)
}
