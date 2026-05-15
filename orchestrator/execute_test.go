package orchestrator

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/niconiahi/the-agent/message"
	"github.com/niconiahi/the-agent/tool"
)

func TestSequential_SingleToolCall(t *testing.T) {
	echo_tool := fake_tool_simple("echo", "echoed")

	agent_context := &AgentContext{
		Tools: []tool.Tool{echo_tool},
	}

	assistant_message := &message.AssistantMessage{
		Content: []message.Content{
			message.ToolCall{ID: "tc_1", Name: "echo", Arguments: map[string]interface{}{}},
		},
	}

	config := &AgentLoopConfig{ToolExecution: TOOL_EXECUTION_SEQUENTIAL}

	results, _, error := execute_tool_calls(context.Background(), agent_context, assistant_message, config, func(_ AgentEvent) {})

	if error != nil {
		t.Fatalf("unexpected error: %v", error)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ToolCallID != "tc_1" {
		t.Errorf("expected tool call ID tc_1, got %s", results[0].ToolCallID)
	}
}

func TestSequential_UnknownTool(t *testing.T) {
	agent_context := &AgentContext{
		Tools: []tool.Tool{},
	}

	assistant_message := &message.AssistantMessage{
		Content: []message.Content{
			message.ToolCall{ID: "tc_1", Name: "nonexistent", Arguments: map[string]interface{}{}},
		},
	}

	config := &AgentLoopConfig{ToolExecution: TOOL_EXECUTION_SEQUENTIAL}

	results, _, error := execute_tool_calls(context.Background(), agent_context, assistant_message, config, func(_ AgentEvent) {})

	if error != nil {
		t.Fatalf("unexpected error: %v", error)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !results[0].IsError {
		t.Error("expected IsError true for unknown tool")
	}
}

func TestSequential_ToolReturnsError(t *testing.T) {
	failing_tool := fake_tool_with_error("fail", fmt.Errorf("tool broke"))

	agent_context := &AgentContext{
		Tools: []tool.Tool{failing_tool},
	}

	assistant_message := &message.AssistantMessage{
		Content: []message.Content{
			message.ToolCall{ID: "tc_1", Name: "fail", Arguments: map[string]interface{}{}},
		},
	}

	config := &AgentLoopConfig{ToolExecution: TOOL_EXECUTION_SEQUENTIAL}

	results, _, error := execute_tool_calls(context.Background(), agent_context, assistant_message, config, func(_ AgentEvent) {})

	if error != nil {
		t.Fatalf("unexpected error: %v", error)
	}
	if !results[0].IsError {
		t.Error("expected IsError true when tool returns error")
	}
}

func TestSequential_BeforeHookBlocks(t *testing.T) {
	echo_tool := fake_tool_simple("echo", "should not run")

	agent_context := &AgentContext{
		Tools: []tool.Tool{echo_tool},
	}

	assistant_message := &message.AssistantMessage{
		Content: []message.Content{
			message.ToolCall{ID: "tc_1", Name: "echo", Arguments: map[string]interface{}{}},
		},
	}

	config := &AgentLoopConfig{
		ToolExecution: TOOL_EXECUTION_SEQUENTIAL,
		BeforeToolCall: func(_ context.Context, _ BeforeToolCallContext) *BeforeToolCallResult {
			return &BeforeToolCallResult{Block: true, Reason: "blocked by policy"}
		},
	}

	results, _, error := execute_tool_calls(context.Background(), agent_context, assistant_message, config, func(_ AgentEvent) {})

	if error != nil {
		t.Fatalf("unexpected error: %v", error)
	}
	if !results[0].IsError {
		t.Error("expected IsError true when blocked by hook")
	}
}

func TestSequential_AfterHookOverrides(t *testing.T) {
	echo_tool := fake_tool_simple("echo", "original")

	agent_context := &AgentContext{
		Tools: []tool.Tool{echo_tool},
	}

	assistant_message := &message.AssistantMessage{
		Content: []message.Content{
			message.ToolCall{ID: "tc_1", Name: "echo", Arguments: map[string]interface{}{}},
		},
	}

	override_error := true
	config := &AgentLoopConfig{
		ToolExecution: TOOL_EXECUTION_SEQUENTIAL,
		AfterToolCall: func(_ context.Context, _ AfterToolCallContext) *AfterToolCallResult {
			return &AfterToolCallResult{
				Content: []message.Content{message.TextContent{Text: "overridden"}},
				Details: "custom details",
				IsError: &override_error,
			}
		},
	}

	results, _, error := execute_tool_calls(context.Background(), agent_context, assistant_message, config, func(_ AgentEvent) {})

	if error != nil {
		t.Fatalf("unexpected error: %v", error)
	}
	if !results[0].IsError {
		t.Error("expected IsError overridden to true")
	}
	if tc, ok := results[0].Content[0].(message.TextContent); !ok || tc.Text != "overridden" {
		t.Error("expected content overridden")
	}
}

func TestSequential_SteeringSkipsRemaining(t *testing.T) {
	steering_delivered := false
	var calls []call_record
	var mutex sync.Mutex

	tool_a := fake_tool_recording("a", tool.ToolResult{Content: []message.Content{message.TextContent{Text: "a_result"}}}, &calls, &mutex)
	tool_b := fake_tool_recording("b", tool.ToolResult{Content: []message.Content{message.TextContent{Text: "b_result"}}}, &calls, &mutex)

	agent_context := &AgentContext{
		Tools: []tool.Tool{tool_a, tool_b},
	}

	assistant_message := &message.AssistantMessage{
		Content: []message.Content{
			message.ToolCall{ID: "tc_1", Name: "a", Arguments: map[string]interface{}{}},
			message.ToolCall{ID: "tc_2", Name: "b", Arguments: map[string]interface{}{}},
		},
	}

	config := &AgentLoopConfig{
		ToolExecution: TOOL_EXECUTION_SEQUENTIAL,
		GetSteeringMessages: func() []message.Message {
			if !steering_delivered {
				steering_delivered = true
				return []message.Message{
					message.UserMessage{Content: []message.Content{message.TextContent{Text: "steer!"}}, Timestamp: time.Now()},
				}
			}
			return nil
		},
	}

	results, steering, error := execute_tool_calls(context.Background(), agent_context, assistant_message, config, func(_ AgentEvent) {})

	if error != nil {
		t.Fatalf("unexpected error: %v", error)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[1].IsError != true {
		t.Error("expected second tool skipped with IsError")
	}
	if len(steering) == 0 {
		t.Error("expected steering messages")
	}

	mutex.Lock()
	if len(calls) != 1 {
		t.Errorf("expected only 1 tool execution, got %d", len(calls))
	}
	mutex.Unlock()
}

func TestSequential_MultipleToolsAllExecute(t *testing.T) {
	var calls []call_record
	var mutex sync.Mutex

	tool_a := fake_tool_recording("a", tool.ToolResult{Content: []message.Content{message.TextContent{Text: "a"}}}, &calls, &mutex)
	tool_b := fake_tool_recording("b", tool.ToolResult{Content: []message.Content{message.TextContent{Text: "b"}}}, &calls, &mutex)

	agent_context := &AgentContext{
		Tools: []tool.Tool{tool_a, tool_b},
	}

	assistant_message := &message.AssistantMessage{
		Content: []message.Content{
			message.ToolCall{ID: "tc_1", Name: "a", Arguments: map[string]interface{}{}},
			message.ToolCall{ID: "tc_2", Name: "b", Arguments: map[string]interface{}{}},
		},
	}

	config := &AgentLoopConfig{ToolExecution: TOOL_EXECUTION_SEQUENTIAL}

	results, _, error := execute_tool_calls(context.Background(), agent_context, assistant_message, config, func(_ AgentEvent) {})

	if error != nil {
		t.Fatalf("unexpected error: %v", error)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	mutex.Lock()
	if len(calls) != 2 {
		t.Errorf("expected 2 tool executions, got %d", len(calls))
	}
	mutex.Unlock()
}

func TestSequential_EventsEmitted(t *testing.T) {
	echo_tool := fake_tool_simple("echo", "done")

	agent_context := &AgentContext{
		Tools: []tool.Tool{echo_tool},
	}

	assistant_message := &message.AssistantMessage{
		Content: []message.Content{
			message.ToolCall{ID: "tc_1", Name: "echo", Arguments: map[string]interface{}{}},
		},
	}

	config := &AgentLoopConfig{ToolExecution: TOOL_EXECUTION_SEQUENTIAL}

	var event_types []string
	results, _, error := execute_tool_calls(context.Background(), agent_context, assistant_message, config, func(event AgentEvent) {
		event_types = append(event_types, event.AgentEventType())
	})

	if error != nil {
		t.Fatalf("unexpected error: %v", error)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	has_start := false
	has_end := false
	for _, et := range event_types {
		if et == "tool_execution_start" {
			has_start = true
		}
		if et == "tool_execution_end" {
			has_end = true
		}
	}
	if !has_start || !has_end {
		t.Errorf("expected tool_execution_start and tool_execution_end events, got: %v", event_types)
	}
}

func TestParallel_AllToolsExecute(t *testing.T) {
	var calls []call_record
	var mutex sync.Mutex

	tool_a := fake_tool_recording("a", tool.ToolResult{Content: []message.Content{message.TextContent{Text: "a"}}}, &calls, &mutex)
	tool_b := fake_tool_recording("b", tool.ToolResult{Content: []message.Content{message.TextContent{Text: "b"}}}, &calls, &mutex)

	agent_context := &AgentContext{
		Tools: []tool.Tool{tool_a, tool_b},
	}

	assistant_message := &message.AssistantMessage{
		Content: []message.Content{
			message.ToolCall{ID: "tc_1", Name: "a", Arguments: map[string]interface{}{}},
			message.ToolCall{ID: "tc_2", Name: "b", Arguments: map[string]interface{}{}},
		},
	}

	config := &AgentLoopConfig{ToolExecution: TOOL_EXECUTION_PARALLEL}

	results, _, error := execute_tool_calls(context.Background(), agent_context, assistant_message, config, func(_ AgentEvent) {})

	if error != nil {
		t.Fatalf("unexpected error: %v", error)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	mutex.Lock()
	if len(calls) != 2 {
		t.Errorf("expected 2 tool executions, got %d", len(calls))
	}
	mutex.Unlock()
}

func TestParallel_BeforeHookBlocksOne(t *testing.T) {
	tool_a := fake_tool_simple("a", "a_result")
	tool_b := fake_tool_simple("b", "b_result")

	agent_context := &AgentContext{
		Tools: []tool.Tool{tool_a, tool_b},
	}

	assistant_message := &message.AssistantMessage{
		Content: []message.Content{
			message.ToolCall{ID: "tc_1", Name: "a", Arguments: map[string]interface{}{}},
			message.ToolCall{ID: "tc_2", Name: "b", Arguments: map[string]interface{}{}},
		},
	}

	config := &AgentLoopConfig{
		ToolExecution: TOOL_EXECUTION_PARALLEL,
		BeforeToolCall: func(_ context.Context, before_context BeforeToolCallContext) *BeforeToolCallResult {
			if before_context.ToolCall.Name == "a" {
				return &BeforeToolCallResult{Block: true, Reason: "blocked"}
			}
			return nil
		},
	}

	results, _, error := execute_tool_calls(context.Background(), agent_context, assistant_message, config, func(_ AgentEvent) {})

	if error != nil {
		t.Fatalf("unexpected error: %v", error)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if !results[0].IsError {
		t.Error("expected first result blocked with IsError")
	}
	if results[1].IsError {
		t.Error("expected second result to succeed")
	}
}

func TestParallel_ConcurrentExecution(t *testing.T) {
	tool_a := fake_tool_slow("a", 100*time.Millisecond, "a")
	tool_b := fake_tool_slow("b", 100*time.Millisecond, "b")

	agent_context := &AgentContext{
		Tools: []tool.Tool{tool_a, tool_b},
	}

	assistant_message := &message.AssistantMessage{
		Content: []message.Content{
			message.ToolCall{ID: "tc_1", Name: "a", Arguments: map[string]interface{}{}},
			message.ToolCall{ID: "tc_2", Name: "b", Arguments: map[string]interface{}{}},
		},
	}

	config := &AgentLoopConfig{ToolExecution: TOOL_EXECUTION_PARALLEL}

	start := time.Now()
	results, _, error := execute_tool_calls(context.Background(), agent_context, assistant_message, config, func(_ AgentEvent) {})
	elapsed := time.Since(start)

	if error != nil {
		t.Fatalf("unexpected error: %v", error)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if elapsed > 180*time.Millisecond {
		t.Errorf("expected parallel execution in ~100ms, took %v", elapsed)
	}
}

func TestParallel_ResultsInOriginalOrder(t *testing.T) {
	tool_a := fake_tool_slow("a", 80*time.Millisecond, "a_result")
	tool_b := fake_tool_slow("b", 10*time.Millisecond, "b_result")

	agent_context := &AgentContext{
		Tools: []tool.Tool{tool_a, tool_b},
	}

	assistant_message := &message.AssistantMessage{
		Content: []message.Content{
			message.ToolCall{ID: "tc_1", Name: "a", Arguments: map[string]interface{}{}},
			message.ToolCall{ID: "tc_2", Name: "b", Arguments: map[string]interface{}{}},
		},
	}

	config := &AgentLoopConfig{ToolExecution: TOOL_EXECUTION_PARALLEL}

	results, _, error := execute_tool_calls(context.Background(), agent_context, assistant_message, config, func(_ AgentEvent) {})

	if error != nil {
		t.Fatalf("unexpected error: %v", error)
	}
	if results[0].ToolCallID != "tc_1" {
		t.Errorf("expected first result to be tc_1, got %s", results[0].ToolCallID)
	}
	if results[1].ToolCallID != "tc_2" {
		t.Errorf("expected second result to be tc_2, got %s", results[1].ToolCallID)
	}
}
