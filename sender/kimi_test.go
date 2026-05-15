package sender

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/niconiahi/the-agent/message"
)

func TestParseSSE_SingleDataLine(t *testing.T) {
	input := "data: {\"content\":\"hi\"}\n"
	results := drain_sse(input)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0] != `{"content":"hi"}` {
		t.Fatalf("expected '{\"content\":\"hi\"}', got '%s'", results[0])
	}
}

func TestParseSSE_MultipleDataLines(t *testing.T) {
	input := "data: first\ndata: second\ndata: third\n"
	results := drain_sse(input)

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	if results[0] != "first" || results[1] != "second" || results[2] != "third" {
		t.Fatalf("unexpected results: %v", results)
	}
}

func TestParseSSE_SkipsEmptyLines(t *testing.T) {
	input := "data: first\n\n\ndata: second\n"
	results := drain_sse(input)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
}

func TestParseSSE_SkipsCommentLines(t *testing.T) {
	input := ": this is a comment\ndata: actual\n"
	results := drain_sse(input)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0] != "actual" {
		t.Fatalf("expected 'actual', got '%s'", results[0])
	}
}

func TestParseSSE_DoneTerminates(t *testing.T) {
	input := "data: first\ndata: [DONE]\ndata: should_not_appear\n"
	results := drain_sse(input)

	if len(results) != 1 {
		t.Fatalf("expected 1 result (before [DONE]), got %d", len(results))
	}
}

func TestParseSSE_IgnoresNonDataLines(t *testing.T) {
	input := "event: message\ndata: payload\nid: 123\n"
	results := drain_sse(input)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0] != "payload" {
		t.Fatalf("expected 'payload', got '%s'", results[0])
	}
}

func TestParseSSE_EmptyInput(t *testing.T) {
	results := drain_sse("")

	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}

func TestParseSSE_LargePayload(t *testing.T) {
	large := strings.Repeat("x", 500_000)
	input := "data: " + large + "\n"
	results := drain_sse(input)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if len(results[0]) != 500_000 {
		t.Fatalf("expected 500000 bytes, got %d", len(results[0]))
	}
}

func TestConvertUserMessage_TextOnly(t *testing.T) {
	input := message.UserMessage{
		Content: []message.Content{message.TextContent{Text: "hello"}},
	}

	result := convert_user_message(input)

	content_str, ok := result.Content.(string)
	if !ok {
		t.Fatalf("expected string content for text-only message, got %T", result.Content)
	}
	if content_str != "hello" {
		t.Fatalf("expected 'hello', got '%s'", content_str)
	}
}

func TestConvertUserMessage_WithImage(t *testing.T) {
	input := message.UserMessage{
		Content: []message.Content{
			message.TextContent{Text: "look at this"},
			message.ImageContent{Data: "abc123", MimeType: "image/png"},
		},
	}

	result := convert_user_message(input)

	parts, ok := result.Content.([]openai_content_part)
	if !ok {
		t.Fatalf("expected []openai_content_part for image message, got %T", result.Content)
	}
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(parts))
	}
	if parts[0].Type != "text" || parts[0].Text != "look at this" {
		t.Errorf("first part should be text, got %+v", parts[0])
	}
	if parts[1].Type != "image_url" || parts[1].ImageURL == nil {
		t.Fatalf("second part should be image_url, got %+v", parts[1])
	}
	if parts[1].ImageURL.URL != "data:image/png;base64,abc123" {
		t.Errorf("expected data URI, got '%s'", parts[1].ImageURL.URL)
	}
}

func TestConvertUserMessage_MultipleTextBlocks(t *testing.T) {
	input := message.UserMessage{
		Content: []message.Content{
			message.TextContent{Text: "hello"},
			message.TextContent{Text: " world"},
		},
	}

	result := convert_user_message(input)

	content_str, ok := result.Content.(string)
	if !ok {
		t.Fatalf("expected string content, got %T", result.Content)
	}
	if content_str != "hello world" {
		t.Fatalf("expected 'hello world', got '%s'", content_str)
	}
}

func TestConvertAssistantMessage_TextAndThinking(t *testing.T) {
	input := message.AssistantMessage{
		Content: []message.Content{
			message.ThinkingContent{Thinking: "let me think"},
			message.TextContent{Text: "here's my answer"},
		},
	}

	result := convert_assistant_message(input)

	if result.ReasoningContent != "let me think" {
		t.Errorf("expected reasoning 'let me think', got '%s'", result.ReasoningContent)
	}
	content_str, ok := result.Content.(string)
	if !ok {
		t.Fatalf("expected string content, got %T", result.Content)
	}
	if content_str != "here's my answer" {
		t.Errorf("expected 'here's my answer', got '%s'", content_str)
	}
}

func TestConvertAssistantMessage_WithToolCalls(t *testing.T) {
	input := message.AssistantMessage{
		Content: []message.Content{
			message.ToolCall{
				ID:        "tc_1",
				Name:      "read",
				Arguments: map[string]interface{}{"path": "/tmp/foo"},
			},
		},
	}

	result := convert_assistant_message(input)

	if len(result.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(result.ToolCalls))
	}
	tc := result.ToolCalls[0]
	if tc.ID != "tc_1" {
		t.Errorf("expected ID 'tc_1', got '%s'", tc.ID)
	}
	if tc.Function.Name != "read" {
		t.Errorf("expected name 'read', got '%s'", tc.Function.Name)
	}

	var args map[string]interface{}
	if json.Unmarshal([]byte(tc.Function.Arguments), &args) != nil {
		t.Fatalf("failed to parse arguments JSON: %s", tc.Function.Arguments)
	}
	if args["path"] != "/tmp/foo" {
		t.Errorf("expected path '/tmp/foo', got '%v'", args["path"])
	}
}

func TestConvertAssistantMessage_MixedContent(t *testing.T) {
	input := message.AssistantMessage{
		Content: []message.Content{
			message.ThinkingContent{Thinking: "hmm"},
			message.TextContent{Text: "response"},
			message.ToolCall{ID: "tc_1", Name: "bash", Arguments: map[string]interface{}{"command": "ls"}},
		},
	}

	result := convert_assistant_message(input)

	if result.ReasoningContent != "hmm" {
		t.Errorf("expected reasoning 'hmm', got '%s'", result.ReasoningContent)
	}
	content_str, ok := result.Content.(string)
	if !ok {
		t.Fatalf("expected string content, got %T", result.Content)
	}
	if content_str != "response" {
		t.Errorf("expected 'response', got '%s'", content_str)
	}
	if len(result.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(result.ToolCalls))
	}
}

func TestConvertAssistantMessage_EmptyContent(t *testing.T) {
	input := message.AssistantMessage{Content: []message.Content{}}

	result := convert_assistant_message(input)

	if result.Content != nil {
		t.Errorf("expected nil content, got %v", result.Content)
	}
	if result.ReasoningContent != "" {
		t.Errorf("expected empty reasoning, got '%s'", result.ReasoningContent)
	}
	if len(result.ToolCalls) != 0 {
		t.Errorf("expected no tool calls, got %d", len(result.ToolCalls))
	}
}

func TestConvertToolResultMessage_TextResult(t *testing.T) {
	input := message.ToolResultMessage{
		ToolCallID: "tc_1",
		Content:    []message.Content{message.TextContent{Text: "file contents here"}},
	}

	result := convert_tool_result_message(input)

	if result.Role != "tool" {
		t.Errorf("expected role 'tool', got '%s'", result.Role)
	}
	if result.ToolCallID != "tc_1" {
		t.Errorf("expected tool_call_id 'tc_1', got '%s'", result.ToolCallID)
	}
	content_str, ok := result.Content.(string)
	if !ok {
		t.Fatalf("expected string content, got %T", result.Content)
	}
	if content_str != "file contents here" {
		t.Errorf("expected 'file contents here', got '%s'", content_str)
	}
}

func drain_sse(input string) []string {
	data_channel := make(chan string, 64)
	sse_error := make(chan error, 1)

	go parse_sse(strings.NewReader(input), data_channel, sse_error)

	var results []string
	for data := range data_channel {
		results = append(results, data)
	}
	return results
}
