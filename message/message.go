package message

import "time"

type Message interface {
	is_message()
}

type UserMessage struct {
	Content   []Content `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

func (UserMessage) is_message() {}

type AssistantMessage struct {
	Content      []Content  `json:"content"`
	API          string     `json:"api"`
	Provider     string     `json:"provider"`
	Model        string     `json:"model"`
	Usage        Usage      `json:"usage"`
	StopReason   StopReason `json:"stop_reason"`
	ErrorMessage string     `json:"error_message,omitempty"`
	Timestamp    time.Time  `json:"timestamp"`
}

func (AssistantMessage) is_message() {}

type ToolResultMessage struct {
	ToolCallID string    `json:"tool_call_id"`
	ToolName   string    `json:"tool_name"`
	Content    []Content `json:"content"`
	Details    any       `json:"details,omitempty"`
	IsError    bool      `json:"is_error"`
	Timestamp  time.Time `json:"timestamp"`
}

func (ToolResultMessage) is_message() {}
