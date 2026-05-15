package chat

import (
	"github.com/niconiahi/the-agent/message"
)

type AgentStartMsg struct{}

type AgentEndMsg struct {
	Messages []message.Message
}

type TextDeltaMsg struct {
	Delta string
}

type ThinkingDeltaMsg struct {
	Delta string
}

type ToolCallEndMsg struct {
	ToolCall message.ToolCall
}

type ToolExecStartMsg struct {
	ToolCallID string
	ToolName   string
	Arguments  map[string]interface{}
}

type ToolExecEndMsg struct {
	ToolCallID string
	ToolName   string
	IsError    bool
}

type MessageEndMsg struct {
	Message message.Message
}

type PromptDoneMsg struct {
	Error error
}
