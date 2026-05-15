package orchestrator

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/niconiahi/the-agent/message"
	"github.com/niconiahi/the-agent/sender"
	"github.com/niconiahi/the-agent/tool"
)

func TestRunLoop_SingleTurnNoTools(t *testing.T) {
	cleanup := register_fake_provider([]*message.AssistantMessage{
		text_response("hello"),
	})
	defer cleanup()

	agent_context := &AgentContext{
		Messages: []message.Message{
			message.UserMessage{Content: []message.Content{message.TextContent{Text: "hi"}}, Timestamp: time.Now()},
		},
	}

	config := &AgentLoopConfig{
		Model:   test_model(),
		Options: &sender.StreamOptions{},
	}

	var events []AgentEvent
	messages, error := run_loop(context.Background(), agent_context, config, func(event AgentEvent) {
		events = append(events, event)
	})

	if error != nil {
		t.Fatalf("unexpected error: %v", error)
	}
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages (user + assistant), got %d", len(messages))
	}
}

func TestRunLoop_ToolCallLoop(t *testing.T) {
	cleanup := register_fake_provider([]*message.AssistantMessage{
		tool_call_response(message.ToolCall{ID: "tc_1", Name: "echo", Arguments: map[string]interface{}{}}),
		text_response("done"),
	})
	defer cleanup()

	echo_tool := fake_tool_simple("echo", "echoed")

	agent_context := &AgentContext{
		Messages: []message.Message{
			message.UserMessage{Content: []message.Content{message.TextContent{Text: "use echo"}}, Timestamp: time.Now()},
		},
		Tools: []tool.Tool{echo_tool},
	}

	config := &AgentLoopConfig{
		Model:         test_model(),
		Options:       &sender.StreamOptions{},
		ToolExecution: TOOL_EXECUTION_SEQUENTIAL,
	}

	messages, error := run_loop(context.Background(), agent_context, config, func(_ AgentEvent) {})

	if error != nil {
		t.Fatalf("unexpected error: %v", error)
	}
	if len(messages) != 4 {
		t.Fatalf("expected 4 messages (user + assistant_tool_call + tool_result + assistant_text), got %d", len(messages))
	}
}

func TestRunLoop_ErrorStopReason(t *testing.T) {
	cleanup := register_fake_provider([]*message.AssistantMessage{
		error_response("API failure"),
	})
	defer cleanup()

	agent_context := &AgentContext{
		Messages: []message.Message{
			message.UserMessage{Content: []message.Content{message.TextContent{Text: "hi"}}, Timestamp: time.Now()},
		},
	}

	config := &AgentLoopConfig{
		Model:   test_model(),
		Options: &sender.StreamOptions{},
	}

	_, error := run_loop(context.Background(), agent_context, config, func(_ AgentEvent) {})

	if error == nil {
		t.Fatal("expected error for error stop reason")
	}
}

func TestRunLoop_AbortedStopReason(t *testing.T) {
	cleanup := register_fake_provider([]*message.AssistantMessage{
		aborted_response(),
	})
	defer cleanup()

	agent_context := &AgentContext{
		Messages: []message.Message{
			message.UserMessage{Content: []message.Content{message.TextContent{Text: "hi"}}, Timestamp: time.Now()},
		},
	}

	config := &AgentLoopConfig{
		Model:   test_model(),
		Options: &sender.StreamOptions{},
	}

	_, error := run_loop(context.Background(), agent_context, config, func(_ AgentEvent) {})

	if error == nil {
		t.Fatal("expected error for aborted stop reason")
	}
}

func TestRunLoop_SteeringInterrupt(t *testing.T) {
	steering_delivered := false

	cleanup := register_fake_provider([]*message.AssistantMessage{
		tool_call_response(message.ToolCall{ID: "tc_1", Name: "slow", Arguments: map[string]interface{}{}}),
		text_response("after steering"),
	})
	defer cleanup()

	slow_tool := fake_tool_simple("slow", "done")

	agent_context := &AgentContext{
		Messages: []message.Message{
			message.UserMessage{Content: []message.Content{message.TextContent{Text: "do something"}}, Timestamp: time.Now()},
		},
		Tools: []tool.Tool{slow_tool},
	}

	config := &AgentLoopConfig{
		Model:         test_model(),
		Options:       &sender.StreamOptions{},
		ToolExecution: TOOL_EXECUTION_SEQUENTIAL,
		GetSteeringMessages: func() []message.Message {
			if !steering_delivered {
				steering_delivered = true
				return []message.Message{
					message.UserMessage{Content: []message.Content{message.TextContent{Text: "interrupt!"}}, Timestamp: time.Now()},
				}
			}
			return nil
		},
	}

	messages, error := run_loop(context.Background(), agent_context, config, func(_ AgentEvent) {})

	if error != nil {
		t.Fatalf("unexpected error: %v", error)
	}

	has_steering := false
	for _, msg := range messages {
		if user_message, ok := msg.(message.UserMessage); ok {
			if len(user_message.Content) > 0 {
				if tc, ok := user_message.Content[0].(message.TextContent); ok && tc.Text == "interrupt!" {
					has_steering = true
				}
			}
		}
	}
	if !has_steering {
		t.Error("expected steering message in conversation")
	}
}

func TestRunLoop_FollowUpContinuation(t *testing.T) {
	follow_up_delivered := false

	cleanup := register_fake_provider([]*message.AssistantMessage{
		text_response("first response"),
		text_response("second response"),
	})
	defer cleanup()

	agent_context := &AgentContext{
		Messages: []message.Message{
			message.UserMessage{Content: []message.Content{message.TextContent{Text: "start"}}, Timestamp: time.Now()},
		},
	}

	config := &AgentLoopConfig{
		Model:   test_model(),
		Options: &sender.StreamOptions{},
		GetFollowUpMessages: func() []message.Message {
			if !follow_up_delivered {
				follow_up_delivered = true
				return []message.Message{
					message.UserMessage{Content: []message.Content{message.TextContent{Text: "follow up"}}, Timestamp: time.Now()},
				}
			}
			return nil
		},
	}

	messages, error := run_loop(context.Background(), agent_context, config, func(_ AgentEvent) {})

	if error != nil {
		t.Fatalf("unexpected error: %v", error)
	}
	if len(messages) < 4 {
		t.Fatalf("expected at least 4 messages (user + assistant + follow_up + assistant), got %d", len(messages))
	}
}

func TestRunLoop_ConvertToLLMApplied(t *testing.T) {
	convert_called := false

	cleanup := register_fake_provider([]*message.AssistantMessage{
		text_response("ok"),
	})
	defer cleanup()

	agent_context := &AgentContext{
		Messages: []message.Message{
			message.UserMessage{Content: []message.Content{message.TextContent{Text: "test"}}, Timestamp: time.Now()},
		},
	}

	config := &AgentLoopConfig{
		Model:   test_model(),
		Options: &sender.StreamOptions{},
		ConvertToLLM: func(messages []message.Message) []message.Message {
			convert_called = true
			return messages
		},
	}

	_, error := run_loop(context.Background(), agent_context, config, func(_ AgentEvent) {})

	if error != nil {
		t.Fatalf("unexpected error: %v", error)
	}
	if !convert_called {
		t.Error("ConvertToLLM was not called")
	}
}

func TestRunLoop_GetAPIKeyError(t *testing.T) {
	cleanup := register_fake_provider([]*message.AssistantMessage{})
	defer cleanup()

	agent_context := &AgentContext{
		Messages: []message.Message{
			message.UserMessage{Content: []message.Content{message.TextContent{Text: "test"}}, Timestamp: time.Now()},
		},
	}

	config := &AgentLoopConfig{
		Model:   test_model(),
		Options: &sender.StreamOptions{},
		GetAPIKey: func(_ string) (string, error) {
			return "", fmt.Errorf("key not found")
		},
	}

	_, error := run_loop(context.Background(), agent_context, config, func(_ AgentEvent) {})

	if error == nil {
		t.Fatal("expected error from GetAPIKey failure")
	}
}

func TestRunLoop_EventSequence(t *testing.T) {
	cleanup := register_fake_provider([]*message.AssistantMessage{
		text_response("hello"),
	})
	defer cleanup()

	agent_context := &AgentContext{
		Messages: []message.Message{
			message.UserMessage{Content: []message.Content{message.TextContent{Text: "hi"}}, Timestamp: time.Now()},
		},
	}

	config := &AgentLoopConfig{
		Model:   test_model(),
		Options: &sender.StreamOptions{},
	}

	var event_types []string
	_, error := run_loop(context.Background(), agent_context, config, func(event AgentEvent) {
		event_types = append(event_types, event.AgentEventType())
	})

	if error != nil {
		t.Fatalf("unexpected error: %v", error)
	}

	if len(event_types) < 5 {
		t.Fatalf("expected at least 5 events, got %d: %v", len(event_types), event_types)
	}

	if event_types[0] != "agent_start" {
		t.Errorf("first event should be agent_start, got %s", event_types[0])
	}
	if event_types[len(event_types)-1] != "agent_end" {
		t.Errorf("last event should be agent_end, got %s", event_types[len(event_types)-1])
	}
}
