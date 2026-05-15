package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/niconiahi/the-agent/message"
)

func GrepTool() Tool {
	parameters := json.RawMessage(`{
		"type": "object",
		"properties": {
			"pattern": {"type": "string", "description": "The regex pattern to search for"},
			"path": {"type": "string", "description": "Directory or file to search in"},
			"glob": {"type": "string", "description": "File glob filter"},
			"ignore_case": {"type": "boolean", "description": "Case insensitive search"},
			"limit": {"type": "integer", "description": "Maximum number of results"}
		},
		"required": ["pattern"]
	}`)

	return NewTool("grep", "Search file contents using ripgrep", parameters, execute_grep)
}

func execute_grep(invocation_context context.Context, _ string, arguments map[string]interface{}) (ToolResult, error) {
	pattern, ok := arguments["pattern"].(string)
	if !ok {
		return ToolResult{}, fmt.Errorf("pattern is required")
	}

	search_path := "."
	if raw_path, ok := arguments["path"].(string); ok {
		search_path = raw_path
	}

	limit := 100
	if raw_limit, ok := arguments["limit"].(float64); ok {
		limit = int(raw_limit)
	}

	args := []string{"--no-heading", "--line-number", "--color=never", fmt.Sprintf("--max-count=%d", limit)}

	if ignore_case, ok := arguments["ignore_case"].(bool); ok && ignore_case {
		args = append(args, "--ignore-case")
	}

	if glob, ok := arguments["glob"].(string); ok {
		args = append(args, "--glob", glob)
	}

	args = append(args, pattern, search_path)

	command := exec.CommandContext(invocation_context, "rg", args...)
	var stdout_buffer bytes.Buffer
	var stderr_buffer bytes.Buffer
	command.Stdout = &stdout_buffer
	command.Stderr = &stderr_buffer

	error := command.Run()
	if error != nil {
		if exit_error, ok := error.(*exec.ExitError); ok && exit_error.ExitCode() == 1 {
			return ToolResult{
				Content: []message.Content{message.TextContent{Text: "No matches found"}},
			}, nil
		}
		stderr := strings.TrimSpace(stderr_buffer.String())
		if stderr != "" {
			return ToolResult{}, fmt.Errorf("rg failed: %s", stderr)
		}
		return ToolResult{}, fmt.Errorf("rg failed: %v", error)
	}

	output := stdout_buffer.String()
	if len(output) > MAX_OUTPUT_BYTES {
		output = output[:MAX_OUTPUT_BYTES] + "\n(output truncated)"
	}

	return ToolResult{
		Content: []message.Content{message.TextContent{Text: output}},
	}, nil
}
