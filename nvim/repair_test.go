package nvim_test

import (
	"strings"
	"testing"

	"github.com/niconiahi/the-agent/message"
	"github.com/niconiahi/the-agent/nvim"
	"github.com/niconiahi/the-agent/nvim/nvimtest"
)

const (
	TOOL_CALL_BLOCK   = "```tool_call id=tc_1 name=read ts=2026-10-06T14:32:00Z\n{\"path\":\"a.txt\"}\n```\n\n"
	TOOL_RESULT_BLOCK = "```tool_result id=tc_1 ts=2026-10-06T14:32:00Z\ncontents of a.txt\n```\n\n"
)

func sent_tool_blocks(provider *nvimtest.Provider) (calls int, results int) {
	for _, value := range provider.Requests()[0].Messages {
		switch typed := value.(type) {
		case message.AssistantMessage:
			for _, content := range typed.Content {
				if _, ok := content.(message.ToolCall); ok {
					calls++
				}
			}
		case message.ToolResultMessage:
			results++
		}
	}
	return calls, results
}

func TestTASend_DeletedToolResultRemovesItsCallFromRequestAndFile(t *testing.T) {
	provider := nvimtest.RegisterProvider(t, nvimtest.Text("you're welcome", 10))
	config := nvimtest.Config()
	config.Now = fixed_clock("2026-10-06T14:33:00Z")
	harness := nvimtest.Start(t, config)
	harness.WriteFile(SESSION, strings.Replace(TOOL_TURN, TOOL_RESULT_BLOCK, "", 1)+"thanks\n")

	harness.Command("TA foo")
	harness.Command("TASend")
	harness.WaitFor("the repaired reply on disk", func() bool {
		disk := on_disk(harness)
		return strings.Contains(disk, "you're welcome") && !strings.Contains(disk, "tool_call")
	})

	if calls, results := sent_tool_blocks(provider); calls != 0 || results != 0 {
		t.Fatalf("want no tool call or result sent, got %d calls and %d results", calls, results)
	}
	if got := harness.Text(); strings.Contains(got, "tool_call") {
		t.Errorf("the buffer still has the orphaned tool_call:\n%s", got)
	}
}

func TestTASend_DeletedToolCallRemovesItsResultAndUndoBringsItBack(t *testing.T) {
	provider := nvimtest.RegisterProvider(t, nvimtest.Reply{
		Deltas:      []string{"you're ", "very ", "welcome"},
		Delay:       3 * nvim.FLUSH_INTERVAL,
		TotalTokens: 10,
	})
	config := nvimtest.Config()
	config.Now = fixed_clock("2026-10-06T14:33:00Z")
	harness := nvimtest.Start(t, config)
	harness.WriteFile(SESSION, strings.Replace(TOOL_TURN, TOOL_CALL_BLOCK, "", 1)+"thanks\n")

	harness.Command("TA foo")
	harness.Command("TASend")
	harness.WaitFor("the repaired reply on disk", func() bool {
		disk := on_disk(harness)
		return strings.Contains(disk, "you're very welcome") && !strings.Contains(disk, "tool_result")
	})

	if calls, results := sent_tool_blocks(provider); calls != 0 || results != 0 {
		t.Fatalf("want no tool call or result sent, got %d calls and %d results", calls, results)
	}
	if got := harness.Text(); strings.Contains(got, "tool_result") {
		t.Errorf("the buffer still has the orphaned tool_result:\n%s", got)
	}

	harness.Command("normal! u")
	got := harness.Text()
	if !strings.Contains(got, TOOL_RESULT_BLOCK) {
		t.Fatalf("one u must bring back the removed tool_result, got:\n%s", got)
	}
	if !strings.Contains(got, "you're very welcome") {
		t.Fatalf("one u must only undo the repair, but the reply is gone:\n%s", got)
	}
}
