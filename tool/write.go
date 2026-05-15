package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/niconiahi/the-agent/message"
)

func WriteTool() Tool {
	parameters := json.RawMessage(`{
		"type": "object",
		"properties": {
			"path": {"type": "string", "description": "Absolute path to the file to write"},
			"content": {"type": "string", "description": "The content to write to the file"}
		},
		"required": ["path", "content"]
	}`)

	return NewTool("write", "Create or overwrite a file", parameters, execute_write)
}

func execute_write(_ context.Context, _ string, arguments map[string]any) (ToolResult, error) {
	path, ok := arguments["path"].(string)
	if !ok {
		return ToolResult{}, fmt.Errorf("path is required")
	}

	content, ok := arguments["content"].(string)
	if !ok {
		return ToolResult{}, fmt.Errorf("content is required")
	}

	error := os.MkdirAll(filepath.Dir(path), 0755)
	if error != nil {
		return ToolResult{}, fmt.Errorf("failed to create directories: %v", error)
	}

	error = os.WriteFile(path, []byte(content), 0644)
	if error != nil {
		return ToolResult{}, fmt.Errorf("failed to write file: %v", error)
	}

	return ToolResult{
		Content: []message.Content{message.TextContent{Text: fmt.Sprintf("Wrote %s", path)}},
	}, nil
}
