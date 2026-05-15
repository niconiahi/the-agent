package tool

import (
	"context"
	"encoding/json"

	"github.com/niconiahi/the-agent/message"
	"github.com/niconiahi/the-agent/sender"
)

type ToolResult struct {
	Content []message.Content
	Details interface{}
}

type ExecuteFunction func(invocation_context context.Context, tool_call_id string, arguments map[string]interface{}) (ToolResult, error)

type Tool struct {
	Name        string
	Description string
	Parameters  json.RawMessage
	Execute     ExecuteFunction
}

func NewTool(name string, description string, parameters json.RawMessage, execute ExecuteFunction) Tool {
	return Tool{
		Name:        name,
		Description: description,
		Parameters:  parameters,
		Execute:     execute,
	}
}

func ToSchema(input Tool) sender.ToolSchema {
	return sender.ToolSchema{
		Name:        input.Name,
		Description: input.Description,
		Parameters:  input.Parameters,
	}
}
