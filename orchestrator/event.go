package orchestrator

import (
	"github.com/niconiahi/the-agent/message"
	"github.com/niconiahi/the-agent/sender"
)

type AgentEvent interface {
	is_agent_event()
	AgentEventType() string
}

type AgentStartEvent struct{}

func (AgentStartEvent) is_agent_event()          {}
func (AgentStartEvent) AgentEventType() string   { return "agent_start" }

type AgentEndEvent struct {
	Messages []message.Message
}

func (AgentEndEvent) is_agent_event()          {}
func (AgentEndEvent) AgentEventType() string   { return "agent_end" }

type TurnStartEvent struct{}

func (TurnStartEvent) is_agent_event()          {}
func (TurnStartEvent) AgentEventType() string   { return "turn_start" }

type TurnEndEvent struct {
	AssistantMessage *message.AssistantMessage
	ToolResults      []message.ToolResultMessage
}

func (TurnEndEvent) is_agent_event()          {}
func (TurnEndEvent) AgentEventType() string   { return "turn_end" }

type MessageStartEvent struct {
	Message message.Message
}

func (MessageStartEvent) is_agent_event()          {}
func (MessageStartEvent) AgentEventType() string   { return "message_start" }

type MessageUpdateEvent struct {
	SenderEvent sender.Event
}

func (MessageUpdateEvent) is_agent_event()          {}
func (MessageUpdateEvent) AgentEventType() string   { return "message_update" }

type MessageEndEvent struct {
	Message message.Message
}

func (MessageEndEvent) is_agent_event()          {}
func (MessageEndEvent) AgentEventType() string   { return "message_end" }

type ToolExecutionStartEvent struct {
	ToolCallID string
	ToolName   string
	Arguments  map[string]interface{}
}

func (ToolExecutionStartEvent) is_agent_event()          {}
func (ToolExecutionStartEvent) AgentEventType() string   { return "tool_execution_start" }

type ToolExecutionUpdateEvent struct {
	ToolCallID string
	Result     interface{}
}

func (ToolExecutionUpdateEvent) is_agent_event()          {}
func (ToolExecutionUpdateEvent) AgentEventType() string   { return "tool_execution_update" }

type ToolExecutionEndEvent struct {
	ToolCallID string
	ToolName   string
	Result     interface{}
	IsError    bool
}

func (ToolExecutionEndEvent) is_agent_event()          {}
func (ToolExecutionEndEvent) AgentEventType() string   { return "tool_execution_end" }
