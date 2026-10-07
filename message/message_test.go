package message_test

import (
	"testing"

	"github.com/niconiahi/the-agent/message"
)

func TestContentOf_ReturnsEveryMessageKindsContent(t *testing.T) {
	text := []message.Content{message.TextContent{Text: "hello"}}
	cases := map[string]message.Message{
		"user":                 message.UserMessage{Content: text},
		"assistant":            message.AssistantMessage{Content: text},
		"assistant by pointer": &message.AssistantMessage{Content: text},
		"tool result":          message.ToolResultMessage{Content: text},
	}
	for name, current := range cases {
		got := message.ContentOf(current)
		if len(got) != 1 || got[0] != text[0] {
			t.Errorf("%s: got %#v", name, got)
		}
	}
	if got := message.ContentOf(nil); got != nil {
		t.Errorf("nil: got %#v", got)
	}
}
