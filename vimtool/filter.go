package vimtool

import (
	"context"
	"encoding/json"
	"fmt"

	neovim "github.com/neovim/go-client/nvim"

	"github.com/niconiahi/the-agent/message"
	"github.com/niconiahi/the-agent/tool"
)

func Filter(client *neovim.Nvim) tool.Tool {
	parameters := json.RawMessage(`{
		"type": "object",
		"properties": {
			"path": {"type": "string", "description": "Path to the file to filter, absolute or relative to the project"},
			"command": {"type": "string", "description": "A shell command that reads the file on stdin and prints its new content, e.g. sort, gofmt or a sed expression"}
		},
		"required": ["path", "command"]
	}`)

	description := "Run a text-in, text-out shell command over a whole file, like :%!command in Vim, replace the file with its output and save it. A command that exits non-zero changes nothing"
	return tool.NewTool("filter", description, parameters,
		func(invocation_context context.Context, _ string, arguments map[string]any) (tool.ToolResult, error) {
			path, ok := arguments["path"].(string)
			if !ok {
				return tool.ToolResult{}, fmt.Errorf("path is required")
			}
			command, ok := arguments["command"].(string)
			if !ok {
				return tool.ToolResult{}, fmt.Errorf("command is required")
			}
			if error := change(client, invocation_context, "filter", path, command); error != nil {
				return tool.ToolResult{}, error
			}
			text := fmt.Sprintf("Filtered %s through %s", path, command)
			return tool.ToolResult{Content: []message.Content{message.TextContent{Text: text}}}, nil
		})
}
