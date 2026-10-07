package orchestrator

import (
	"github.com/niconiahi/the-agent/message"
	"github.com/niconiahi/the-agent/sender"
)

// AgentEvent is everything an Agent emits. Every event names the agent that
// emitted it, so one listener can route the events of several agents (root
// sessions running side by side, or subagents forwarded by a parent).
type AgentEvent interface {
	is_agent_event()
	AgentEventType() string
	AgentID() string
	from(Source) AgentEvent
}

// Source identifies the agent that emitted an event. The Agent fills it in;
// code building events leaves it empty.
type Source struct {
	Agent string
}

func (source Source) AgentID() string { return source.Agent }

type AgentStartEvent struct {
	Source
}

func (AgentStartEvent) is_agent_event()        {}
func (AgentStartEvent) AgentEventType() string { return "agent_start" }

type AgentEndEvent struct {
	Source
	Messages []message.Message
}

func (AgentEndEvent) is_agent_event()        {}
func (AgentEndEvent) AgentEventType() string { return "agent_end" }

type TurnStartEvent struct {
	Source
}

func (TurnStartEvent) is_agent_event()        {}
func (TurnStartEvent) AgentEventType() string { return "turn_start" }

type TurnEndEvent struct {
	Source
	AssistantMessage *message.AssistantMessage
	ToolResults      []message.ToolResultMessage
}

func (TurnEndEvent) is_agent_event()        {}
func (TurnEndEvent) AgentEventType() string { return "turn_end" }

type MessageStartEvent struct {
	Source
	Message message.Message
}

func (MessageStartEvent) is_agent_event()        {}
func (MessageStartEvent) AgentEventType() string { return "message_start" }

type MessageUpdateEvent struct {
	Source
	SenderEvent sender.Event
}

func (MessageUpdateEvent) is_agent_event()        {}
func (MessageUpdateEvent) AgentEventType() string { return "message_update" }

type MessageEndEvent struct {
	Source
	Message message.Message
}

func (MessageEndEvent) is_agent_event()        {}
func (MessageEndEvent) AgentEventType() string { return "message_end" }

type ToolExecutionStartEvent struct {
	Source
	ToolCallID string
	ToolName   string
	Arguments  map[string]interface{}
}

func (ToolExecutionStartEvent) is_agent_event()        {}
func (ToolExecutionStartEvent) AgentEventType() string { return "tool_execution_start" }

type ToolExecutionUpdateEvent struct {
	Source
	ToolCallID string
	Result     interface{}
}

func (ToolExecutionUpdateEvent) is_agent_event()        {}
func (ToolExecutionUpdateEvent) AgentEventType() string { return "tool_execution_update" }

type ToolExecutionEndEvent struct {
	Source
	ToolCallID string
	ToolName   string
	Result     interface{}
	IsError    bool
}

func (ToolExecutionEndEvent) is_agent_event()        {}
func (ToolExecutionEndEvent) AgentEventType() string { return "tool_execution_end" }

func (event AgentEndEvent) from(source Source) AgentEvent { event.Source = source; return event }

func (event TurnEndEvent) from(source Source) AgentEvent { event.Source = source; return event }

func (event MessageStartEvent) from(source Source) AgentEvent { event.Source = source; return event }

func (event MessageUpdateEvent) from(source Source) AgentEvent { event.Source = source; return event }

func (event MessageEndEvent) from(source Source) AgentEvent { event.Source = source; return event }

func (event ToolExecutionStartEvent) from(source Source) AgentEvent {
	event.Source = source
	return event
}

func (event ToolExecutionUpdateEvent) from(source Source) AgentEvent {
	event.Source = source
	return event
}

func (event ToolExecutionEndEvent) from(source Source) AgentEvent {
	event.Source = source
	return event
}

func (event AgentStartEvent) from(source Source) AgentEvent {
	event.Source = source
	return event
}

func (event TurnStartEvent) from(source Source) AgentEvent {
	event.Source = source
	return event
}
