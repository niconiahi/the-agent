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

func FindTool() Tool {
	parameters := json.RawMessage(`{
		"type": "object",
		"properties": {
			"pattern": {"type": "string", "description": "Glob pattern to search for"},
			"path": {"type": "string", "description": "Directory to search in"},
			"limit": {"type": "integer", "description": "Maximum number of results"}
		},
		"required": ["pattern"]
	}`)

	return NewTool("find", "Search for files by glob pattern using fd", parameters, execute_find)
}

func execute_find(invocation_context context.Context, _ string, arguments map[string]interface{}) (ToolResult, error) {
	pattern, ok := arguments["pattern"].(string)
	if !ok {
		return ToolResult{}, fmt.Errorf("pattern is required")
	}

	search_path := "."
	if raw_path, ok := arguments["path"].(string); ok {
		search_path = raw_path
	}

	limit := 1000
	if raw_limit, ok := arguments["limit"].(float64); ok {
		limit = int(raw_limit)
	}

	args := []string{"--color=never", fmt.Sprintf("--max-results=%d", limit), pattern, search_path}

	command := exec.CommandContext(invocation_context, "fd", args...)
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
			return ToolResult{}, fmt.Errorf("fd failed: %s", stderr)
		}
		return ToolResult{}, fmt.Errorf("fd failed: %v", error)
	}

	output := strings.TrimSpace(stdout_buffer.String())
	if output == "" {
		return ToolResult{
			Content: []message.Content{message.TextContent{Text: "No matches found"}},
		}, nil
	}

	if len(output) > MAX_OUTPUT_BYTES {
		output = output[:MAX_OUTPUT_BYTES] + "\n(output truncated)"
	}

	return ToolResult{
		Content: []message.Content{message.TextContent{Text: output}},
	}, nil
}
