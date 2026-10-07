package vimtool

import (
	"context"
	"encoding/json"
	"fmt"

	neovim "github.com/neovim/go-client/nvim"

	"github.com/niconiahi/the-agent/message"
	"github.com/niconiahi/the-agent/tool"
)

func Edit(client *neovim.Nvim) tool.Tool {
	parameters := json.RawMessage(`{
		"type": "object",
		"properties": {
			"path": {"type": "string", "description": "Path to the file to edit, absolute or relative to the project"},
			"old_text": {"type": "string", "description": "The exact text to find and replace; it must occur exactly once"},
			"new_text": {"type": "string", "description": "The replacement text"}
		},
		"required": ["path", "old_text", "new_text"]
	}`)

	description := "Find and replace exact text in a file, then save it. Prefer edit over write for existing files"
	return tool.NewTool("edit", description, parameters,
		func(invocation_context context.Context, _ string, arguments map[string]any) (tool.ToolResult, error) {
			path, error := required_string(arguments, "path")
			if error != nil {
				return tool.ToolResult{}, error
			}
			old_text, error := required_string(arguments, "old_text")
			if error != nil {
				return tool.ToolResult{}, error
			}
			new_text, error := required_string(arguments, "new_text")
			if error != nil {
				return tool.ToolResult{}, error
			}
			diagnostics, error := change_buffer(client, invocation_context, DIAGNOSTICS_WAIT, "edit", path, old_text, new_text)
			if error != nil {
				return tool.ToolResult{}, error
			}
			text := fmt.Sprintf("Edited %s\n\n%s", path, tool.Diff(old_text, new_text))
			text += diagnostics_section(diagnostics)
			return tool.ToolResult{Content: []message.Content{message.TextContent{Text: text}}}, nil
		})
}
