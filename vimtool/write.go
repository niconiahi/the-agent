package vimtool

import (
	"context"
	"encoding/json"
	"fmt"

	neovim "github.com/neovim/go-client/nvim"

	"github.com/niconiahi/the-agent/message"
	"github.com/niconiahi/the-agent/tool"
)

func Write(client *neovim.Nvim) tool.Tool {
	parameters := json.RawMessage(`{
		"type": "object",
		"properties": {
			"path": {"type": "string", "description": "Path to the file to write, absolute or relative to the project"},
			"content": {"type": "string", "description": "The full content of the file"}
		},
		"required": ["path", "content"]
	}`)

	description := "Create a file or replace its whole content, then save it. Use write for new files; to change an existing file prefer edit, which keeps the change small and reviewable"
	return tool.NewTool("write", description, parameters,
		func(invocation_context context.Context, _ string, arguments map[string]any) (tool.ToolResult, error) {
			path, ok := arguments["path"].(string)
			if !ok {
				return tool.ToolResult{}, fmt.Errorf("path is required")
			}
			content, ok := arguments["content"].(string)
			if !ok {
				return tool.ToolResult{}, fmt.Errorf("content is required")
			}
			if error := change(client, invocation_context, "write", path, content); error != nil {
				return tool.ToolResult{}, error
			}
			return tool.ToolResult{Content: []message.Content{message.TextContent{Text: fmt.Sprintf("Wrote %s", path)}}}, nil
		})
}
