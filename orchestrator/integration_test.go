package orchestrator

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/niconiahi/the-agent/message"
	"github.com/niconiahi/the-agent/model"
	"github.com/niconiahi/the-agent/sender"
	"github.com/niconiahi/the-agent/tool"
)

func integration_model() *model.Model {
	m := model.KimiK25()
	return &m
}

func integration_options(t *testing.T) *sender.StreamOptions {
	key := os.Getenv("KIMI_API_KEY")
	if key == "" {
		t.Skip("KIMI_API_KEY not set, skipping integration test")
	}
	return &sender.StreamOptions{APIKey: key}
}

func TestAgent_EndToEnd_TextOnly(t *testing.T) {
	options := integration_options(t)

	agent := New(
		WithModel(integration_model()),
		WithSystemPrompt("respond in one sentence"),
		WithStreamOptions(options),
	)

	events := collect_events(agent)

	error := agent.PromptText(context.Background(), "say hello")
	if error != nil {
		t.Fatalf("unexpected error: %v", error)
	}

	state := agent.State()
	if len(state.Messages) < 2 {
		t.Fatalf("expected at least 2 messages, got %d", len(state.Messages))
	}

	has_assistant := false
	for _, msg := range state.Messages {
		if _, ok := msg.(message.AssistantMessage); ok {
			has_assistant = true
		}
	}
	if !has_assistant {
		t.Error("expected an assistant message in conversation")
	}

	if len(*events) == 0 {
		t.Error("expected events to be emitted")
	}
}

func TestAgent_EndToEnd_WithTools(t *testing.T) {
	options := integration_options(t)

	ls_tool := tool.LsTool()

	agent := New(
		WithModel(integration_model()),
		WithSystemPrompt("use tools when asked. respond in one sentence after getting results."),
		WithTools([]tool.Tool{ls_tool}),
		WithStreamOptions(options),
		WithToolExecution(TOOL_EXECUTION_SEQUENTIAL),
	)

	error := agent.PromptText(context.Background(), "list files in /tmp")
	if error != nil {
		t.Fatalf("unexpected error: %v", error)
	}

	state := agent.State()

	has_tool_result := false
	for _, msg := range state.Messages {
		if _, ok := msg.(message.ToolResultMessage); ok {
			has_tool_result = true
		}
	}
	if !has_tool_result {
		t.Error("expected a tool result message in conversation")
	}
}

func TestAgent_EndToEnd_MultiTurnTools(t *testing.T) {
	options := integration_options(t)

	ls_tool := tool.LsTool()
	read_tool := tool.ReadTool()

	agent := New(
		WithModel(integration_model()),
		WithSystemPrompt("use tools when asked. you can chain tool calls. respond briefly."),
		WithTools([]tool.Tool{ls_tool, read_tool}),
		WithStreamOptions(options),
		WithToolExecution(TOOL_EXECUTION_SEQUENTIAL),
	)

	error := agent.PromptText(context.Background(), "list files in /tmp and then read the first file you find, if any")
	if error != nil {
		t.Fatalf("unexpected error: %v", error)
	}

	state := agent.State()

	tool_result_count := 0
	for _, msg := range state.Messages {
		if _, ok := msg.(message.ToolResultMessage); ok {
			tool_result_count++
		}
	}

	if tool_result_count == 0 {
		t.Error("expected at least one tool result")
	}
	t.Logf("tool results: %d, total messages: %d", tool_result_count, len(state.Messages))
}

func TestAgent_EndToEnd_Abort(t *testing.T) {
	options := integration_options(t)

	agent := New(
		WithModel(integration_model()),
		WithSystemPrompt("write a very long story. take your time."),
		WithStreamOptions(options),
	)

	done := make(chan error, 1)
	go func() {
		done <- agent.PromptText(context.Background(), "write 10000 words about the history of computing")
	}()

	time.Sleep(500 * time.Millisecond)
	agent.Abort()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Error("Prompt should have returned after Abort")
	}
}
