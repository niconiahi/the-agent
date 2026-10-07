package session

import (
	"time"

	"github.com/niconiahi/the-agent/message"
)

// The pieces of an assistant turn, rendered one at a time for frontends that
// write a turn into the file while it streams. Each is a fenced block without
// a trailing newline, exactly as AppendAssistantMessage and AppendToolResult
// lay them out.

func ThinkingBlock(thinking string) string {
	return render_thinking(thinking)
}

func ToolCallBlock(call message.ToolCall, at time.Time) (string, error) {
	return render_tool_call(call, at)
}

func ToolResultBlock(result message.ToolResultMessage, at time.Time) string {
	return render_tool_result(result, at)
}
