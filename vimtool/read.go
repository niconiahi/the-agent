package vimtool

import (
	"context"
	"encoding/json"

	neovim "github.com/neovim/go-client/nvim"

	"github.com/niconiahi/the-agent/tool"
)

func Read(client *neovim.Nvim) tool.Tool {
	parameters := json.RawMessage(`{
		"type": "object",
		"properties": {
			"path": {"type": "string", "description": "Path to the file to read, absolute or relative to the project"},
			"offset": {"type": "integer", "description": "Line number to start reading from (1-indexed)"},
			"limit": {"type": "integer", "description": "Maximum number of lines to read"}
		},
		"required": ["path"]
	}`)

	return tool.NewTool("read", "Read file contents as the editor sees them", parameters,
		func(invocation_context context.Context, _ string, arguments map[string]any) (tool.ToolResult, error) {
			path, error := required_string(arguments, "path")
			if error != nil {
				return tool.ToolResult{}, error
			}
			read, error := call_buffer_function(client, "read", path, session_directory(invocation_context))
			if error != nil {
				return tool.ToolResult{}, error
			}
			return tool.Numbered(read.Content, tool.LineRangeFrom(arguments)), nil
		})
}
