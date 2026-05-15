package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"

	"github.com/niconiahi/the-agent/message"
)

const MAX_OUTPUT_BYTES = 100_000

func BashTool() Tool {
	parameters := json.RawMessage(`{
		"type": "object",
		"properties": {
			"command": {"type": "string", "description": "The bash command to execute"},
			"timeout": {"type": "integer", "description": "Timeout in seconds"}
		},
		"required": ["command"]
	}`)

	return NewTool("bash", "Execute a bash command", parameters, execute_bash)
}

func execute_bash(invocation_context context.Context, _ string, arguments map[string]interface{}) (ToolResult, error) {
	command_str, ok := arguments["command"].(string)
	if !ok {
		return ToolResult{}, fmt.Errorf("command is required")
	}

	timeout_seconds := 120
	if raw_timeout, ok := arguments["timeout"].(float64); ok {
		timeout_seconds = int(raw_timeout)
	}

	command_context, cancel := context.WithTimeout(invocation_context, time.Duration(timeout_seconds)*time.Second)
	defer cancel()

	command := exec.CommandContext(command_context, "/bin/bash", "-c", command_str)

	var stdout_buffer bytes.Buffer
	var stderr_buffer bytes.Buffer
	command.Stdout = &stdout_buffer
	command.Stderr = &stderr_buffer

	error := command.Run()

	exit_code := 0
	if error != nil {
		if exit_error, ok := error.(*exec.ExitError); ok {
			exit_code = exit_error.ExitCode()
		} else {
			return ToolResult{}, fmt.Errorf("failed to run command: %v", error)
		}
	}

	output := stdout_buffer.String()
	stderr_output := stderr_buffer.String()
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
