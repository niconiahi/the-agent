package session_test

import (
	"testing"

	"github.com/niconiahi/the-agent/message"
	"github.com/niconiahi/the-agent/session"
)

func TestEstimateTokens_CountsSystemPromptAndMessagesAtFourBytesPerToken(t *testing.T) {
	messages := []message.Message{
		message.UserMessage{Content: []message.Content{message.TextContent{Text: "hello world!"}}}, // 12 bytes
		message.AssistantMessage{Content: []message.Content{message.TextContent{Text: "hi"}}},      // 2 bytes
	}
	// 4 + 12 + 2 = 18 bytes, rounded up to 5 tokens.
	if got := session.EstimateTokens("abcd", messages); got != 5 {
		t.Fatalf("want 5, got %d", got)
	}
}

func TestEstimateTokens_EmptyIsZero(t *testing.T) {
	if got := session.EstimateTokens("", nil); got != 0 {
		t.Fatalf("want 0, got %d", got)
	}
}

func TestCeiling_DefaultsTo200k(t *testing.T) {
	if got := session.Ceiling(0, 262144); got != 200000 {
		t.Fatalf("want 200000, got %d", got)
	}
}

func TestCeiling_UsesConfiguredValue(t *testing.T) {
	if got := session.Ceiling(50000, 262144); got != 50000 {
		t.Fatalf("want 50000, got %d", got)
	}
}

func TestCeiling_NeverAboveContextWindow(t *testing.T) {
	if got := session.Ceiling(500000, 262144); got != 262144 {
		t.Fatalf("want 262144, got %d", got)
	}
	if got := session.Ceiling(0, 128000); got != 128000 {
		t.Fatalf("default must clamp too: want 128000, got %d", got)
	}
}
