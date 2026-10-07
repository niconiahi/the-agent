package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/niconiahi/the-agent/message"
)

func EditTool() Tool {
	parameters := json.RawMessage(`{
		"type": "object",
		"properties": {
			"path": {"type": "string", "description": "Absolute path to the file to edit"},
			"old_text": {"type": "string", "description": "The exact text to find and replace"},
			"new_text": {"type": "string", "description": "The replacement text"}
		},
		"required": ["path", "old_text", "new_text"]
	}`)

	return NewTool("edit", "Find and replace exact text in a file", parameters, execute_edit)
}

func execute_edit(_ context.Context, _ string, arguments map[string]interface{}) (ToolResult, error) {
	path, ok := arguments["path"].(string)
	if !ok {
		return ToolResult{}, fmt.Errorf("path is required")
	}

	old_text, ok := arguments["old_text"].(string)
	if !ok {
		return ToolResult{}, fmt.Errorf("old_text is required")
	}

	new_text, ok := arguments["new_text"].(string)
	if !ok {
		return ToolResult{}, fmt.Errorf("new_text is required")
	}

	data, error := os.ReadFile(path)
	if error != nil {
		return ToolResult{}, fmt.Errorf("failed to read file: %v", error)
	}

	content := string(data)
	count := strings.Count(content, old_text)

	if count == 0 {
		return ToolResult{}, fmt.Errorf("old_text not found in file")
	}
	if count > 1 {
		return ToolResult{}, fmt.Errorf("old_text found %d times, must be unique", count)
	}

	new_content := strings.Replace(content, old_text, new_text, 1)

	error = os.WriteFile(path, []byte(new_content), 0644)
	if error != nil {
		return ToolResult{}, fmt.Errorf("failed to write file: %v", error)
	}

	diff := Diff(old_text, new_text)

	return ToolResult{
		Content: []message.Content{message.TextContent{Text: fmt.Sprintf("Edited %s\n\n%s", path, diff)}},
	}, nil
}

func Diff(old_text string, new_text string) string {
	old_lines := strings.Split(old_text, "\n")
	new_lines := strings.Split(new_text, "\n")

	var builder strings.Builder
	for _, line := range old_lines {
		fmt.Fprintf(&builder, "- %s\n", line)
	}
	for _, line := range new_lines {
		fmt.Fprintf(&builder, "+ %s\n", line)
	}
	return builder.String()
}
