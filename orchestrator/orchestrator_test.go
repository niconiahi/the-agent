package orchestrator

import (
	"testing"

	"github.com/niconiahi/the-agent/message"
)

func TestExtractToolCalls_NoToolCalls(t *testing.T) {
	assistant_message := &message.AssistantMessage{
		Content: []message.Content{message.TextContent{Text: "hello"}},
	}

	calls := extract_tool_calls(assistant_message)

	if len(calls) != 0 {
		t.Fatalf("expected 0 tool calls, got %d", len(calls))
	}
}

func TestExtractToolCalls_Mixed(t *testing.T) {
	assistant_message := &message.AssistantMessage{
		Content: []message.Content{
			message.ToolCall{ID: "tc_1", Name: "read"},
			message.TextContent{Text: "between"},
			message.ToolCall{ID: "tc_2", Name: "bash"},
		},
	}

	calls := extract_tool_calls(assistant_message)

	if len(calls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(calls))
	}
	if calls[0].ID != "tc_1" || calls[1].ID != "tc_2" {
		t.Errorf("expected tc_1 and tc_2, got %s and %s", calls[0].ID, calls[1].ID)
	}
}

func TestExtractToolCalls_NilMessage(t *testing.T) {
	calls := extract_tool_calls(nil)

	if calls != nil {
		t.Fatalf("expected nil, got %v", calls)
	}
}
