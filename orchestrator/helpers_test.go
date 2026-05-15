package orchestrator

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/niconiahi/the-agent/message"
	"github.com/niconiahi/the-agent/model"
	"github.com/niconiahi/the-agent/sender"
	"github.com/niconiahi/the-agent/tool"
)

func test_model() *model.Model {
	return &model.Model{
		ID:       "test-model",
		Name:     "Test Model",
		API:      "test-orchestrator",
		Provider: "test",
	}
}

type call_record struct {
	ToolCallID string
	Arguments  map[string]interface{}
}

func fake_tool_recording(name string, result tool.ToolResult, calls *[]call_record, mutex *sync.Mutex) tool.Tool {
	return tool.NewTool(name, "test tool", json.RawMessage(`{"type":"object"}`), func(_ context.Context, tool_call_id string, arguments map[string]interface{}) (tool.ToolResult, error) {
		if mutex != nil {
			mutex.Lock()
			*calls = append(*calls, call_record{ToolCallID: tool_call_id, Arguments: arguments})
			mutex.Unlock()
		}
		return result, nil
	})
}

func fake_tool_simple(name string, output string) tool.Tool {
	return tool.NewTool(name, "test tool", json.RawMessage(`{"type":"object"}`), func(_ context.Context, _ string, _ map[string]interface{}) (tool.ToolResult, error) {
		return tool.ToolResult{
			Content: []message.Content{message.TextContent{Text: output}},
		}, nil
	})
}

func fake_tool_with_error(name string, target_error error) tool.Tool {
	return tool.NewTool(name, "test tool", json.RawMessage(`{"type":"object"}`), func(_ context.Context, _ string, _ map[string]interface{}) (tool.ToolResult, error) {
		return tool.ToolResult{}, target_error
	})
}

func fake_tool_slow(name string, duration time.Duration, output string) tool.Tool {
	return tool.NewTool(name, "test tool", json.RawMessage(`{"type":"object"}`), func(invocation_context context.Context, _ string, _ map[string]interface{}) (tool.ToolResult, error) {
		select {
		case <-time.After(duration):
			return tool.ToolResult{
				Content: []message.Content{message.TextContent{Text: output}},
			}, nil
		case <-invocation_context.Done():
			return tool.ToolResult{}, invocation_context.Err()
		}
	})
}

func register_fake_provider(responses []*message.AssistantMessage) func() {
	call_index := 0
	var mutex sync.Mutex

	sender.RegisterProvider(&sender.Provider{
		API: "test-orchestrator",
		StreamFunction: func(_ context.Context, _ *model.Model, _ *sender.LLMContext, _ *sender.StreamOptions) *sender.EventStream {
			stream := sender.NewEventStream()

			mutex.Lock()
			var response *message.AssistantMessage
			if call_index < len(responses) {
				response = responses[call_index]
				call_index++
			} else {
				response = &message.AssistantMessage{
					StopReason: message.STOP_REASON_STOP,
					Timestamp:  time.Now(),
				}
			}
			mutex.Unlock()

			go func() {
				stream.Push(sender.EventStart{Message: response})

				if response.StopReason == message.STOP_REASON_ERROR {
					stream.Push(sender.EventError{StopReason: response.StopReason, Message: response})
				} else if response.StopReason == message.STOP_REASON_ABORTED {
					stream.Push(sender.EventError{StopReason: response.StopReason, Message: response})
				} else {
					stream.Push(sender.EventDone{StopReason: response.StopReason, Message: response})
				}
				stream.Close()
			}()

			return stream
		},
	})

	return func() {
		sender.RegisterProvider(&sender.Provider{API: "test-orchestrator"})
	}
}

func collect_events(agent *Agent) *[]AgentEvent {
	events := &[]AgentEvent{}
	var mutex sync.Mutex

	agent.Subscribe(func(event AgentEvent) {
		mutex.Lock()
		*events = append(*events, event)
		mutex.Unlock()
	})

	return events
}

func text_response(text string) *message.AssistantMessage {
	return &message.AssistantMessage{
		Content:    []message.Content{message.TextContent{Text: text}},
		StopReason: message.STOP_REASON_STOP,
		Timestamp:  time.Now(),
	}
}

func tool_call_response(calls ...message.ToolCall) *message.AssistantMessage {
	content := make([]message.Content, len(calls))
	for i, tc := range calls {
		content[i] = tc
	}
	return &message.AssistantMessage{
		Content:    content,
		StopReason: message.STOP_REASON_TOOL_USE,
		Timestamp:  time.Now(),
	}
}

func error_response(error_message string) *message.AssistantMessage {
	return &message.AssistantMessage{
		StopReason:   message.STOP_REASON_ERROR,
		ErrorMessage: error_message,
		Timestamp:    time.Now(),
	}
}

func aborted_response() *message.AssistantMessage {
	return &message.AssistantMessage{
		StopReason: message.STOP_REASON_ABORTED,
		Timestamp:  time.Now(),
	}
}
