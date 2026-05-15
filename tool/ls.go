package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/niconiahi/the-agent/message"
)

func LsTool() Tool {
	parameters := json.RawMessage(`{
		"type": "object",
		"properties": {
			"path": {"type": "string", "description": "Directory to list"},
			"limit": {"type": "integer", "description": "Maximum number of entries"}
		}
	}`)

	return NewTool("ls", "List directory contents", parameters, execute_ls)
}

func execute_ls(_ context.Context, _ string, arguments map[string]interface{}) (ToolResult, error) {
	dir_path := "."
	if raw_path, ok := arguments["path"].(string); ok {
		dir_path = raw_path
	}

	limit := 500
	if raw_limit, ok := arguments["limit"].(float64); ok {
		limit = int(raw_limit)
	}

	entries, error := os.ReadDir(dir_path)
	if error != nil {
		return ToolResult{}, fmt.Errorf("failed to read directory: %v", error)
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			name += "/"
		}
		names = append(names, name)
	}

	sort.Strings(names)

	truncated := ""
	if len(names) > limit {
		names = names[:limit]
		truncated = fmt.Sprintf("\n(truncated: showing %d of %d entries)", limit, len(entries))
	}

	return ToolResult{
		Content: []message.Content{message.TextContent{Text: strings.Join(names, "\n") + truncated}},
	}, nil
}
