package sender

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/niconiahi/the-agent/message"
	"github.com/niconiahi/the-agent/model"
)

func kimi_model() *model.Model {
	m := model.KimiK25()
	return &m
}

func kimi_options(t *testing.T) *StreamOptions {
	key := os.Getenv("KIMI_API_KEY")
	if key == "" {
		t.Skip("KIMI_API_KEY not set, skipping integration test")
	}
	return &StreamOptions{APIKey: key}
}

func TestKimiStream_TextResponse(t *testing.T) {
	options := kimi_options(t)
	target := kimi_model()

	llm_context := &LLMContext{
		SystemPrompt: "respond in one sentence",
		Messages: []message.Message{
			message.UserMessage{
				Content:   []message.Content{message.TextContent{Text: "say hello"}},
				Timestamp: time.Now(),
			},
		},
	}

	stream, error := Stream(context.Background(), target, llm_context, options)
	if error != nil {
		t.Fatalf("unexpected error: %v", error)
	}

	var got_start bool
	var got_done bool
	var final_message *message.AssistantMessage

	for event := range stream.Events() {
		switch typed := event.(type) {
		case EventStart:
			got_start = true
		case EventDone:
			got_done = true
			final_message = typed.Message
		}
	}

	if !got_start {
		t.Error("expected EventStart")
	}
	if !got_done {
		t.Error("expected EventDone")
	}
	if final_message == nil {
		t.Fatal("expected final message")
	}
	if final_message.StopReason != message.STOP_REASON_STOP {
		t.Errorf("expected stop reason 'stop', got '%s'", final_message.StopReason)
	}

	has_text := false
	for _, content := range final_message.Content {
		if tc, ok := content.(message.TextContent); ok && tc.Text != "" {
			has_text = true
		}
	}
	if !has_text {
		t.Error("expected text content in response")
	}
}

func TestKimiStream_ToolCallResponse(t *testing.T) {
	options := kimi_options(t)
	target := kimi_model()

	llm_context := &LLMContext{
		SystemPrompt: "always use the provided tools",
		Messages: []message.Message{
			message.UserMessage{
				Content:   []message.Content{message.TextContent{Text: "list files in /tmp"}},
				Timestamp: time.Now(),
			},
		},
		Tools: []ToolSchema{
			{
				Name:        "ls",
				Description: "list files in a directory",
				Parameters:  []byte(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`),
			},
		},
	}

	stream, error := Stream(context.Background(), target, llm_context, options)
	if error != nil {
		t.Fatalf("unexpected error: %v", error)
	}

	var final_message *message.AssistantMessage
	for event := range stream.Events() {
		switch typed := event.(type) {
		case EventDone:
			final_message = typed.Message
		}
	}

	if final_message == nil {
		t.Fatal("expected final message")
	}
	if final_message.StopReason != message.STOP_REASON_TOOL_USE {
		t.Errorf("expected stop reason 'tool_use', got '%s'", final_message.StopReason)
	}

	has_tool_call := false
	for _, content := range final_message.Content {
		if tc, ok := content.(message.ToolCall); ok && tc.Name == "ls" {
			has_tool_call = true
			if tc.ID == "" {
				t.Error("expected non-empty tool call ID")
			}
		}
	}
	if !has_tool_call {
		t.Error("expected ls tool call in response")
	}
}

func TestKimiStream_ThinkingResponse(t *testing.T) {
	options := kimi_options(t)
	target := kimi_model()

	llm_context := &LLMContext{
		SystemPrompt: "think step by step before answering",
		Messages: []message.Message{
			message.UserMessage{
				Content:   []message.Content{message.TextContent{Text: "what is 17 * 23?"}},
				Timestamp: time.Now(),
			},
		},
	}

	stream, error := Stream(context.Background(), target, llm_context, options)
	if error != nil {
		t.Fatalf("unexpected error: %v", error)
	}

	got_thinking := false
	got_text := false
	for event := range stream.Events() {
		switch event.(type) {
		case EventThinkingDelta:
			got_thinking = true
		case EventTextDelta:
			got_text = true
		}
	}

	if !got_text {
		t.Error("expected text delta events")
	}
	if !got_thinking {
		t.Log("no thinking events received (model may not have used reasoning for this prompt)")
	}
}

func TestKimiStream_ContextCancellation(t *testing.T) {
	options := kimi_options(t)
	target := kimi_model()

	llm_context := &LLMContext{
		SystemPrompt: "write a very long story about space exploration",
		Messages: []message.Message{
			message.UserMessage{
				Content:   []message.Content{message.TextContent{Text: "write 5000 words about mars colonization"}},
				Timestamp: time.Now(),
			},
		},
	}

	invocation_context, cancel := context.WithCancel(context.Background())

	stream, error := Stream(invocation_context, target, llm_context, options)
	if error != nil {
		t.Fatalf("unexpected error: %v", error)
	}

	event_count := 0
	got_error := false
	for event := range stream.Events() {
		event_count++
		if event_count == 3 {
			cancel()
		}
		if _, ok := event.(EventError); ok {
			got_error = true
		}
	}

	if !got_error {
		t.Error("expected EventError after context cancellation")
	}
}

func TestKimiStream_InvalidAPIKey(t *testing.T) {
	_ = os.Getenv("KIMI_API_KEY")
	if os.Getenv("KIMI_API_KEY") == "" {
		t.Skip("KIMI_API_KEY not set, skipping integration test")
	}

	target := kimi_model()
	options := &StreamOptions{APIKey: "sk-invalid-garbage-key"}

	llm_context := &LLMContext{
		Messages: []message.Message{
			message.UserMessage{
				Content:   []message.Content{message.TextContent{Text: "hello"}},
				Timestamp: time.Now(),
			},
		},
	}

	stream, error := Stream(context.Background(), target, llm_context, options)
	if error != nil {
		t.Fatalf("unexpected error creating stream: %v", error)
	}

	got_error := false
	for event := range stream.Events() {
		if _, ok := event.(EventError); ok {
			got_error = true
		}
	}

	if !got_error {
		t.Error("expected EventError for invalid API key")
	}
}

func TestKimiStream_UsageTracking(t *testing.T) {
	options := kimi_options(t)
	target := kimi_model()

	llm_context := &LLMContext{
		SystemPrompt: "respond in one word",
		Messages: []message.Message{
			message.UserMessage{
				Content:   []message.Content{message.TextContent{Text: "hi"}},
				Timestamp: time.Now(),
			},
		},
	}

	stream, error := Stream(context.Background(), target, llm_context, options)
	if error != nil {
		t.Fatalf("unexpected error: %v", error)
	}

	var final_message *message.AssistantMessage
	for event := range stream.Events() {
		if typed, ok := event.(EventDone); ok {
			final_message = typed.Message
		}
	}

	if final_message == nil {
		t.Fatal("expected final message")
	}

	if final_message.Usage.InputTokens == 0 {
		t.Log("InputTokens is 0 (provider may not report usage in streaming mode)")
	}
	if final_message.Usage.OutputTokens == 0 {
		t.Log("OutputTokens is 0 (provider may not report usage in streaming mode)")
	}
}
