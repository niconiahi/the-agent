package orchestrator

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/niconiahi/the-agent/message"
	"github.com/niconiahi/the-agent/sender"
	"github.com/niconiahi/the-agent/tool"
)

func TestAgent_NewWithOptions(t *testing.T) {
	test_tool := fake_tool_simple("test", "ok")
	target := test_model()

	agent := New(
		WithModel(target),
		WithTools([]tool.Tool{test_tool}),
		WithSystemPrompt("you are helpful"),
		WithToolExecution(TOOL_EXECUTION_PARALLEL),
		WithStreamOptions(&sender.StreamOptions{}),
	)

	state := agent.State()
	if state.Model != target {
		t.Error("expected model to be set")
	}
	if len(state.Tools) != 1 {
		t.Errorf("expected 1 tool, got %d", len(state.Tools))
	}
	if state.SystemPrompt != "you are helpful" {
		t.Errorf("expected system prompt 'you are helpful', got '%s'", state.SystemPrompt)
	}
}

func TestAgent_Subscribe_ReceivesEvents(t *testing.T) {
	cleanup := register_fake_provider([]*message.AssistantMessage{
		text_response("hi"),
	})
	defer cleanup()

	agent := New(
		WithModel(test_model()),
		WithStreamOptions(&sender.StreamOptions{}),
	)

	events := collect_events(agent)

	error := agent.PromptText(context.Background(), "hello")
	if error != nil {
		t.Fatalf("unexpected error: %v", error)
	}

	if len(*events) == 0 {
		t.Error("expected to receive events")
	}
}

func TestAgent_Subscribe_Unsubscribe(t *testing.T) {
	cleanup := register_fake_provider([]*message.AssistantMessage{
		text_response("first"),
		text_response("second"),
	})
	defer cleanup()

	agent := New(
		WithModel(test_model()),
		WithStreamOptions(&sender.StreamOptions{}),
	)

	var received_events []AgentEvent
	var mutex sync.Mutex
	unsubscribe := agent.Subscribe(func(event AgentEvent) {
		mutex.Lock()
		received_events = append(received_events, event)
		mutex.Unlock()
	})

	error := agent.PromptText(context.Background(), "first")
	if error != nil {
		t.Fatalf("unexpected error: %v", error)
	}

	mutex.Lock()
	count_before := len(received_events)
	mutex.Unlock()

	unsubscribe()

	error = agent.PromptText(context.Background(), "second")
	if error != nil {
		t.Fatalf("unexpected error: %v", error)
	}

	mutex.Lock()
	count_after := len(received_events)
	mutex.Unlock()

	if count_after != count_before {
		t.Errorf("expected no new events after unsubscribe, got %d more", count_after-count_before)
	}
}

func TestAgent_Steer_QueuesAndDrains(t *testing.T) {
	agent := New()

	agent.Steer(message.UserMessage{
		Content:   []message.Content{message.TextContent{Text: "steer 1"}},
		Timestamp: time.Now(),
	})
	agent.Steer(message.UserMessage{
		Content:   []message.Content{message.TextContent{Text: "steer 2"}},
		Timestamp: time.Now(),
	})

	messages := agent.dequeue_steering()
	if len(messages) != 2 {
		t.Fatalf("expected 2 steering messages, got %d", len(messages))
	}

	messages = agent.dequeue_steering()
	if len(messages) != 0 {
		t.Errorf("expected 0 messages after drain, got %d", len(messages))
	}
}

func TestAgent_FollowUp_QueuesAndDrains(t *testing.T) {
	agent := New()

	agent.FollowUp(message.UserMessage{
		Content:   []message.Content{message.TextContent{Text: "follow 1"}},
		Timestamp: time.Now(),
	})

	messages := agent.dequeue_follow_up()
	if len(messages) != 1 {
		t.Fatalf("expected 1 follow-up message, got %d", len(messages))
	}

	messages = agent.dequeue_follow_up()
	if len(messages) != 0 {
		t.Errorf("expected 0 messages after drain, got %d", len(messages))
	}
}

func TestAgent_WaitForIdle_WhenNotRunning(t *testing.T) {
	agent := New()

	done := make(chan struct{})
	go func() {
		agent.WaitForIdle()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Error("WaitForIdle should return immediately on fresh agent")
	}
}

func TestAgent_Abort_CancelsContext(t *testing.T) {
	slow_tool := fake_tool_slow("slow", 5*time.Second, "done")

	cleanup := register_fake_provider([]*message.AssistantMessage{
		tool_call_response(message.ToolCall{ID: "tc_1", Name: "slow", Arguments: map[string]interface{}{}}),
		text_response("should not reach"),
	})
	defer cleanup()

	agent := New(
		WithModel(test_model()),
		WithTools([]tool.Tool{slow_tool}),
		WithStreamOptions(&sender.StreamOptions{}),
		WithToolExecution(TOOL_EXECUTION_SEQUENTIAL),
	)

	done := make(chan error, 1)
	go func() {
		done <- agent.PromptText(context.Background(), "do something slow")
	}()

	time.Sleep(50 * time.Millisecond)
	agent.Abort()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("Prompt should have returned after Abort")
	}
}
