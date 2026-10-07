package nvim_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/niconiahi/the-agent/message"
	"github.com/niconiahi/the-agent/nvim/nvimtest"
	"github.com/niconiahi/the-agent/tool"
)

// read_tool answers every call with the file's fake contents.
func read_tool() tool.Tool {
	return tool.NewTool("read", "read a file", json.RawMessage(`{"type":"object"}`),
		func(_ context.Context, _ string, arguments map[string]any) (tool.ToolResult, error) {
			return tool.ToolResult{Content: []message.Content{message.TextContent{Text: "contents of " + arguments["path"].(string)}}}, nil
		})
}

const TOOL_TURN = "## user · 2026-10-06T14:32:00Z\n\nwhat is in a.txt?\n\n" +
	"## assistant · fake-model · 2026-10-06T14:32:00Z · 100 tokens\n\n" +
	"```thinking\nI should read it\n```\n\n" +
	"```tool_call id=tc_1 name=read ts=2026-10-06T14:32:00Z\n{\"path\":\"a.txt\"}\n```\n\n" +
	"```tool_result id=tc_1 ts=2026-10-06T14:32:00Z\ncontents of a.txt\n```\n\n" +
	"## assistant · fake-model · 2026-10-06T14:32:00Z · 150 tokens\n\n" +
	"it says hello\n\n" +
	"## user\n\n"

func TestTASend_ToolTurnWritesThinkingCallResultAndAnswer(t *testing.T) {
	provider := nvimtest.RegisterProvider(t,
		nvimtest.Reply{
			Thinking:    []string{"I should ", "read it"},
			ToolCalls:   []message.ToolCall{{ID: "tc_1", Name: "read", Arguments: map[string]any{"path": "a.txt"}}},
			TotalTokens: 100,
		},
		nvimtest.Text("it says hello", 150),
		nvimtest.Text("you're welcome", 200),
	)
	config := nvimtest.Config()
	config.Now = fixed_clock("2026-10-06T14:32:00Z")
	config.Tools = []tool.Tool{read_tool()}
	harness := nvimtest.Start(t, config)

	harness.Command("TA foo")
	harness.SetText("## user\n\nwhat is in a.txt?\n")
	harness.Command("TASend")

	harness.WaitFor("the tool turn on disk", func() bool { return harness.ReadFile(SESSION) == TOOL_TURN })
	if got := harness.Text(); got != TOOL_TURN {
		t.Fatalf("buffer\nwant %q\ngot  %q", TOOL_TURN, got)
	}

	// The next send reads the whole tool exchange back from the file.
	harness.SetText(harness.Text() + "thanks\n")
	harness.Command("TASend")
	harness.WaitFor("third reply", func() bool { return strings.Contains(harness.Text(), "you're welcome") })

	requests := provider.Requests()
	if len(requests) != 3 {
		t.Fatalf("want 3 requests, got %d", len(requests))
	}
	messages := requests[2].Messages
	if len(messages) != 5 {
		t.Fatalf("want user, assistant, tool result, assistant, user; got %#v", messages)
	}
	calling, ok := messages[1].(message.AssistantMessage)
	if !ok || len(calling.Content) != 3 {
		t.Fatalf("want heading text, thinking and tool call, got %#v", messages[1])
	}
	if text, _ := calling.Content[0].(message.TextContent); text.Text != "fake-model · 2026-10-06T14:32:00Z · 100 tokens" {
		t.Errorf("heading text: got %#v", calling.Content[0])
	}
	if thinking, _ := calling.Content[1].(message.ThinkingContent); thinking.Thinking != "I should read it" {
		t.Errorf("thinking: got %#v", calling.Content[1])
	}
	if call, _ := calling.Content[2].(message.ToolCall); call.ID != "tc_1" || call.Name != "read" || call.Arguments["path"] != "a.txt" {
		t.Errorf("tool call: got %#v", calling.Content[2])
	}
	result, ok := messages[2].(message.ToolResultMessage)
	if !ok || result.ToolCallID != "tc_1" {
		t.Fatalf("want the tool result for tc_1, got %#v", messages[2])
	}
}
