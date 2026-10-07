package sender

import "github.com/niconiahi/the-agent/message"

type Event interface {
	is_event()
	EventType() string
}

type EventStart struct {
	Message *message.AssistantMessage
}

func (EventStart) is_event()            {}
func (EventStart) EventType() string    { return "start" }

type EventTextStart struct {
	ContentIndex int
	Message      *message.AssistantMessage
}

func (EventTextStart) is_event()         {}
func (EventTextStart) EventType() string { return "text_start" }

type EventTextDelta struct {
	ContentIndex int
	Delta        string
	Message      *message.AssistantMessage
}

func (EventTextDelta) is_event()         {}
func (EventTextDelta) EventType() string { return "text_delta" }

type EventTextEnd struct {
	ContentIndex int
	FullText     string
	Message      *message.AssistantMessage
}

func (EventTextEnd) is_event()         {}
func (EventTextEnd) EventType() string { return "text_end" }

type EventThinkingStart struct {
	ContentIndex int
	Message      *message.AssistantMessage
}

func (EventThinkingStart) is_event()         {}
func (EventThinkingStart) EventType() string { return "thinking_start" }

type EventThinkingDelta struct {
	ContentIndex int
	Delta        string
	Message      *message.AssistantMessage
}

func (EventThinkingDelta) is_event()         {}
func (EventThinkingDelta) EventType() string { return "thinking_delta" }

type EventThinkingEnd struct {
	ContentIndex int
	FullText     string
	Message      *message.AssistantMessage
}

func (EventThinkingEnd) is_event()         {}
func (EventThinkingEnd) EventType() string { return "thinking_end" }

type EventToolCallStart struct {
	ContentIndex int
	// ID and Name identify the call before its arguments stream.
	ID      string
	Name    string
	Message *message.AssistantMessage
}

func (EventToolCallStart) is_event()         {}
func (EventToolCallStart) EventType() string { return "tool_call_start" }

type EventToolCallDelta struct {
	ContentIndex int
	Delta        string
	Message      *message.AssistantMessage
}

func (EventToolCallDelta) is_event()         {}
func (EventToolCallDelta) EventType() string { return "tool_call_delta" }

type EventToolCallEnd struct {
	ContentIndex int
	ToolCall     message.ToolCall
	Message      *message.AssistantMessage
}

func (EventToolCallEnd) is_event()         {}
func (EventToolCallEnd) EventType() string { return "tool_call_end" }

type EventDone struct {
	StopReason message.StopReason
	Message    *message.AssistantMessage
}

func (EventDone) is_event()         {}
func (EventDone) EventType() string { return "done" }

type EventError struct {
	StopReason message.StopReason
	Message    *message.AssistantMessage
}

func (EventError) is_event()         {}
func (EventError) EventType() string { return "error" }
