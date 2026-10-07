package session

import (
	"time"

	"github.com/niconiahi/the-agent/message"
)

func ThinkingBlock(thinking string) string {
	return render_thinking(thinking)
}

func ToolCallBlock(call message.ToolCall, at time.Time) (string, error) {
	return render_tool_call(call, at)
}

func ToolResultBlock(result message.ToolResultMessage, at time.Time) string {
	return render_tool_result(result, at)
}
