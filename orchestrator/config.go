package orchestrator

import (
	"context"

	"github.com/niconiahi/the-agent/message"
	"github.com/niconiahi/the-agent/model"
	"github.com/niconiahi/the-agent/sender"
	"github.com/niconiahi/the-agent/tool"
)

type ToolExecutionMode string

const (
	TOOL_EXECUTION_SEQUENTIAL ToolExecutionMode = "sequential"
	TOOL_EXECUTION_PARALLEL   ToolExecutionMode = "parallel"
)

type BeforeToolCallContext struct {
	AssistantMessage *message.AssistantMessage
	ToolCall         message.ToolCall
	Arguments        map[string]interface{}
	AgentContext     *AgentContext
}

type BeforeToolCallResult struct {
	Block  bool
	Reason string
}

type AfterToolCallContext struct {
	AssistantMessage *message.AssistantMessage
	ToolCall         message.ToolCall
	Arguments        map[string]interface{}
	Result           tool.ToolResult
	IsError          bool
	AgentContext     *AgentContext
}

type AfterToolCallResult struct {
	Content []message.Content
	Details interface{}
	IsError *bool
}

type AgentContext struct {
	SystemPrompt string
	Messages     []message.Message
	Tools        []tool.Tool
}

type AgentState struct {
	SystemPrompt     string
	Model            *model.Model
	Tools            []tool.Tool
	Messages         []message.Message
	IsStreaming       bool
	StreamMessage    *message.AssistantMessage
	PendingToolCalls map[string]struct{}
	ErrorMessage     string
}

type AgentEventSink func(AgentEvent)

type AgentLoopConfig struct {
	Model              *model.Model
	Options            *sender.StreamOptions
	ConvertToLLM       func([]message.Message) []message.Message
	TransformContext   func(context.Context, []message.Message) []message.Message
	GetAPIKey          func(provider string) (string, error)
	GetSteeringMessages func() []message.Message
	GetFollowUpMessages func() []message.Message
	ToolExecution      ToolExecutionMode
	BeforeToolCall     func(context.Context, BeforeToolCallContext) *BeforeToolCallResult
	AfterToolCall      func(context.Context, AfterToolCallContext) *AfterToolCallResult
}
