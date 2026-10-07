package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/niconiahi/the-agent/message"
)

const MAX_READ_LINES = 5000

func ReadTool() Tool {
	parameters := json.RawMessage(`{
		"type": "object",
		"properties": {
			"path": {"type": "string", "description": "Absolute path to the file to read"},
			"offset": {"type": "integer", "description": "Line number to start reading from (1-indexed)"},
			"limit": {"type": "integer", "description": "Maximum number of lines to read"}
		},
		"required": ["path"]
	}`)

	return NewTool("read", "Read file contents", parameters, execute_read)
}

func execute_read(_ context.Context, _ string, arguments map[string]any) (ToolResult, error) {
	path, ok := arguments["path"].(string)
	if !ok {
		return ToolResult{}, fmt.Errorf("path is required")
	}

	data, error := os.ReadFile(path)
	if error != nil {
		return ToolResult{}, fmt.Errorf("failed to read file: %v", error)
	}

	return Numbered(string(data), arguments), nil
}

func Numbered(content string, arguments map[string]any) ToolResult {
	lines := strings.Split(content, "\n")

	offset := 1
	if raw_offset, ok := arguments["offset"].(float64); ok {
		offset = int(raw_offset)
	}
	if offset < 1 {
		offset = 1
	}

	limit := MAX_READ_LINES
	if raw_limit, ok := arguments["limit"].(float64); ok {
		limit = int(raw_limit)
	}

	start := offset - 1
	if start >= len(lines) {
		return ToolResult{
			Content: []message.Content{message.TextContent{Text: ""}},
		}
	}

	end := start + limit
	if end > len(lines) {
		end = len(lines)
	}

	var builder strings.Builder
	for i := start; i < end; i++ {
		fmt.Fprintf(&builder, "%d\t%s\n", i+1, lines[i])
	}

	truncated := ""
	if end < len(lines) {
		truncated = fmt.Sprintf("\n(truncated: showing lines %d-%d of %d)", offset, end, len(lines))
	}

	return ToolResult{
		Content: []message.Content{message.TextContent{Text: builder.String() + truncated}},
	}
}
