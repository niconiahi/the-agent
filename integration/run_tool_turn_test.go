package integration_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/niconiahi/the-agent/message"
	"github.com/niconiahi/the-agent/nvim/nvimtest"
	"github.com/niconiahi/the-agent/tool"
)

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

func fold_start(t *testing.T, harness *nvimtest.Harness, line int) int {
	t.Helper()
	var start int
	if error := harness.Nvim.Call("foldclosed", &start, line); error != nil {
		t.Fatal(error)
	}
	return start
}

func TestTA_FoldsThinkingClosedAndDapRemovesIt(t *testing.T) {
	provider := nvimtest.RegisterProvider(t, nvimtest.Text("ok", 10))
	harness := nvimtest.Start(t, nvimtest.Config())
	harness.WriteFile(SESSION, TOOL_TURN)

	harness.Command("TA foo")

	for line := 7; line <= 9; line++ {
		if got := fold_start(t, harness, line); got != 7 {
			t.Errorf("line %d: want inside a closed fold starting at 7, got %d", line, got)
		}
	}
	for _, line := range []int{5, 6, 10, 11, 12, 15} {
		if got := fold_start(t, harness, line); got != -1 {
			t.Errorf("line %d: want unfolded, got fold at %d", line, got)
		}
	}

	harness.Command("call cursor(8, 1)")
	harness.Command("normal! dap")
	if strings.Contains(harness.Text(), "thinking") {
		t.Fatalf("dap left the thinking block:\n%s", harness.Text())
	}

	harness.SetText(harness.Text() + "thanks\n")
	harness.Command("TASend")
	harness.WaitFor("reply", func() bool { return strings.Contains(harness.Text(), "\nok\n") })

	for _, value := range provider.Requests()[0].Messages {
		if assistant, ok := value.(message.AssistantMessage); ok {
			for _, content := range assistant.Content {
				if _, ok := content.(message.ThinkingContent); ok {
					t.Fatalf("deleted thinking was sent: %#v", assistant)
				}
			}
		}
	}
}

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
	if got := fold_start(t, harness, 8); got != 7 {
		t.Errorf("the new thinking block must be folded closed, got fold at %d", got)
	}

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
