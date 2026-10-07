package orchestrator

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/niconiahi/the-agent/message"
	"github.com/niconiahi/the-agent/model"
	"github.com/niconiahi/the-agent/sender"
	"github.com/niconiahi/the-agent/tool"
)

type AgentOption func(*Agent)

type Agent struct {
	id              string
	state           AgentState
	mutex           sync.RWMutex
	listeners       []func(AgentEvent)
	cancel_function context.CancelFunc
	steering_queue  []message.Message
	follow_up_queue []message.Message
	idle_channel    chan struct{}

	system_prompt     string
	model_config      *model.Model
	tools             []tool.Tool
	tool_execution    ToolExecutionMode
	convert_to_llm    func([]message.Message) []message.Message
	transform_context func(context.Context, []message.Message) []message.Message
	get_api_key       func(provider string) (string, error)
	before_tool_call  func(context.Context, BeforeToolCallContext) *BeforeToolCallResult
	after_tool_call   func(context.Context, AfterToolCallContext) *AfterToolCallResult
	stream_options    *sender.StreamOptions
}

var unnamed_agents atomic.Int64

func WithID(id string) AgentOption {
	return func(agent *Agent) {
		agent.id = id
	}
}

func WithModel(target *model.Model) AgentOption {
	return func(agent *Agent) {
		agent.model_config = target
	}
}

func WithTools(tools []tool.Tool) AgentOption {
	return func(agent *Agent) {
		agent.tools = tools
	}
}

func WithSystemPrompt(prompt string) AgentOption {
	return func(agent *Agent) {
		agent.system_prompt = prompt
	}
}

func WithToolExecution(mode ToolExecutionMode) AgentOption {
	return func(agent *Agent) {
		agent.tool_execution = mode
	}
}

func WithConvertToLLM(convert func([]message.Message) []message.Message) AgentOption {
	return func(agent *Agent) {
		agent.convert_to_llm = convert
	}
}

func WithTransformContext(transform func(context.Context, []message.Message) []message.Message) AgentOption {
	return func(agent *Agent) {
		agent.transform_context = transform
	}
}

func WithBeforeToolCall(hook func(context.Context, BeforeToolCallContext) *BeforeToolCallResult) AgentOption {
	return func(agent *Agent) {
		agent.before_tool_call = hook
	}
}

func WithAfterToolCall(hook func(context.Context, AfterToolCallContext) *AfterToolCallResult) AgentOption {
	return func(agent *Agent) {
		agent.after_tool_call = hook
	}
}

func WithStreamOptions(options *sender.StreamOptions) AgentOption {
	return func(agent *Agent) {
		agent.stream_options = options
	}
}

func New(options ...AgentOption) *Agent {
	agent := &Agent{
		tool_execution: TOOL_EXECUTION_SEQUENTIAL,
		idle_channel:   make(chan struct{}),
		state: AgentState{
			PendingToolCalls: map[string]struct{}{},
		},
	}

	for _, option := range options {
		option(agent)
	}
	if agent.id == "" {
		agent.id = fmt.Sprintf("agent-%d", unnamed_agents.Add(1))
	}

	agent.state.SystemPrompt = agent.system_prompt
	agent.state.Model = agent.model_config
	agent.state.Tools = agent.tools

	close(agent.idle_channel)

	return agent
}

func (agent *Agent) Prompt(invocation_context context.Context, input message.Message) (returned_error error) {
	agent.mutex.Lock()
	agent.idle_channel = make(chan struct{})
	agent.state.Messages = append(agent.state.Messages, input)
	agent.mutex.Unlock()

	loop_context, cancel := context.WithCancel(invocation_context)
	agent.mutex.Lock()
	agent.cancel_function = cancel
	agent.mutex.Unlock()

	defer func() {
		if r := recover(); r != nil {
			returned_error = fmt.Errorf("agent panicked: %v", r)
		}
		cancel()
		agent.mutex.Lock()
		close(agent.idle_channel)
		agent.mutex.Unlock()
	}()

	agent_context := &AgentContext{
		SystemPrompt: agent.system_prompt,
		Messages:     agent.state.Messages,
		Tools:        agent.tools,
	}

	loop_config := &AgentLoopConfig{
		Model:               agent.model_config,
		Options:             agent.stream_options,
		ConvertToLLM:        agent.convert_to_llm,
		TransformContext:    agent.transform_context,
		GetAPIKey:           agent.get_api_key,
		GetSteeringMessages: agent.dequeue_steering,
		GetFollowUpMessages: agent.dequeue_follow_up,
		ToolExecution:       agent.tool_execution,
		BeforeToolCall:      agent.before_tool_call,
		AfterToolCall:       agent.after_tool_call,
	}

	result_messages, error := run_loop(loop_context, agent_context, loop_config, agent.process_event)

	agent.mutex.Lock()
	agent.state.Messages = result_messages
	agent.mutex.Unlock()

	return error
}

func (agent *Agent) PromptText(invocation_context context.Context, text string) error {
	return agent.Prompt(invocation_context, message.UserMessage{
		Content:   []message.Content{message.TextContent{Text: text}},
		Timestamp: time.Now(),
	})
}

func (agent *Agent) ID() string {
	return agent.id
}

func (agent *Agent) Abort() {
	agent.mutex.RLock()
	cancel := agent.cancel_function
	agent.mutex.RUnlock()

	if cancel != nil {
		cancel()
	}
}

func (agent *Agent) Subscribe(listener func(AgentEvent)) func() {
	agent.mutex.Lock()
	defer agent.mutex.Unlock()

	agent.listeners = append(agent.listeners, listener)
	index := len(agent.listeners) - 1

	return func() {
		agent.mutex.Lock()
		defer agent.mutex.Unlock()
		agent.listeners[index] = nil
	}
}

func (agent *Agent) Steer(input message.Message) {
	agent.mutex.Lock()
	defer agent.mutex.Unlock()
	agent.steering_queue = append(agent.steering_queue, input)
}

func (agent *Agent) FollowUp(input message.Message) {
	agent.mutex.Lock()
	defer agent.mutex.Unlock()
	agent.follow_up_queue = append(agent.follow_up_queue, input)
}

func (agent *Agent) State() AgentState {
	agent.mutex.RLock()
	defer agent.mutex.RUnlock()
	return agent.state
}

func (agent *Agent) WaitForIdle() {
	agent.mutex.RLock()
	idle := agent.idle_channel
	agent.mutex.RUnlock()
	<-idle
}

func (agent *Agent) emit(event AgentEvent) {
	agent.mutex.RLock()
	listeners := make([]func(AgentEvent), len(agent.listeners))
	copy(listeners, agent.listeners)
	agent.mutex.RUnlock()

	for _, listener := range listeners {
		if listener != nil {
			listener(event)
		}
	}
}

func (agent *Agent) process_event(event AgentEvent) {
	event = event.from(Source{Agent: agent.id})
	agent.mutex.Lock()
	switch typed := event.(type) {
	case MessageStartEvent:
		if assistant_message, ok := typed.Message.(*message.AssistantMessage); ok {
			agent.state.IsStreaming = true
			agent.state.StreamMessage = assistant_message
		}
	case MessageEndEvent:
		agent.state.IsStreaming = false
		agent.state.StreamMessage = nil
	case ToolExecutionStartEvent:
		agent.state.PendingToolCalls[typed.ToolCallID] = struct{}{}
	case ToolExecutionEndEvent:
		delete(agent.state.PendingToolCalls, typed.ToolCallID)
	case AgentEndEvent:
		agent.state.Messages = typed.Messages
	}
	agent.mutex.Unlock()

	agent.emit(event)
}

func (agent *Agent) dequeue_steering() []message.Message {
	agent.mutex.Lock()
	defer agent.mutex.Unlock()

	messages := agent.steering_queue
	agent.steering_queue = nil
	return messages
}

func (agent *Agent) dequeue_follow_up() []message.Message {
	agent.mutex.Lock()
	defer agent.mutex.Unlock()

	messages := agent.follow_up_queue
	agent.follow_up_queue = nil
	return messages
}
