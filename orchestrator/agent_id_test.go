package orchestrator

import (
	"context"
	"testing"

	"github.com/niconiahi/the-agent/message"
	"github.com/niconiahi/the-agent/sender"
	"github.com/niconiahi/the-agent/tool"
)

func TestAgent_EveryEventCarriesTheAgentID(t *testing.T) {
	for _, mode := range []ToolExecutionMode{TOOL_EXECUTION_SEQUENTIAL, TOOL_EXECUTION_PARALLEL} {
		t.Run(string(mode), func(t *testing.T) {
			cleanup := register_fake_provider([]*message.AssistantMessage{
				tool_call_response(
					message.ToolCall{ID: "call-1", Name: "first", Arguments: map[string]interface{}{}},
					message.ToolCall{ID: "call-2", Name: "missing", Arguments: map[string]interface{}{}},
				),
				text_response("done"),
			})
			defer cleanup()

			agent := New(
				WithID("sessions/foo"),
				WithModel(test_model()),
				WithTools([]tool.Tool{fake_tool_simple("first", "ok")}),
				WithToolExecution(mode),
				WithStreamOptions(&sender.StreamOptions{}),
			)
			events := collect_events(agent)

			if error := agent.PromptText(context.Background(), "hello"); error != nil {
				t.Fatalf("unexpected error: %v", error)
			}

			seen := map[string]bool{}
			for _, event := range *events {
				seen[event.AgentEventType()] = true
				if got := event.AgentID(); got != "sessions/foo" {
					t.Errorf("%s event: agent ID %q, want %q", event.AgentEventType(), got, "sessions/foo")
				}
			}
			for _, kind := range []string{"agent_start", "agent_end", "turn_start", "turn_end", "message_start", "message_end", "tool_execution_start", "tool_execution_end"} {
				if !seen[kind] {
					t.Errorf("no %s event was emitted", kind)
				}
			}
		})
	}
}

func TestAgent_AgentsWithoutAnIDGetDistinctOnes(t *testing.T) {
	first, second := New(), New()

	if first.ID() == "" || second.ID() == "" {
		t.Fatalf("empty agent IDs: %q, %q", first.ID(), second.ID())
	}
	if first.ID() == second.ID() {
		t.Fatalf("two agents share the ID %q", first.ID())
	}
	if got := New(WithID("given")).ID(); got != "given" {
		t.Fatalf("ID() = %q, want %q", got, "given")
	}
}
