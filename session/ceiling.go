package session

import (
	"encoding/json"

	"github.com/niconiahi/the-agent/message"
)

// DEFAULT_CEILING is the token ceiling of a session when none is configured.
const DEFAULT_CEILING = 200_000

// BYTES_PER_TOKEN is the estimator's ratio. There is no tokenizer for the
// model, and provider usage only arrives after a request, so the count that
// guards a request is an estimate.
const BYTES_PER_TOKEN = 4

// IMAGE_TOKENS is what an image counts for in the estimate.
const IMAGE_TOKENS = 1_000

// Ceiling is the token ceiling for a session: configured, or DEFAULT_CEILING
// when configured is not positive, and never above the model's context window.
func Ceiling(configured int, context_window int) int {
	ceiling := configured
	if ceiling <= 0 {
		ceiling = DEFAULT_CEILING
	}
	return min(ceiling, context_window)
}

// EstimateTokens estimates the tokens a request with this system prompt and
// these messages costs.
func EstimateTokens(system_prompt string, messages []message.Message) int {
	bytes := len(system_prompt)
	images := 0
	for _, current := range messages {
		for _, content := range message_content(current) {
			switch typed := content.(type) {
			case message.TextContent:
				bytes += len(typed.Text)
			case message.ThinkingContent:
				bytes += len(typed.Thinking)
			case message.ToolCall:
				arguments, _ := json.Marshal(typed.Arguments)
				bytes += len(typed.Name) + len(arguments)
			case message.ImageContent:
				images++
			}
		}
	}
	return (bytes+BYTES_PER_TOKEN-1)/BYTES_PER_TOKEN + images*IMAGE_TOKENS
}

func message_content(current message.Message) []message.Content {
	switch typed := current.(type) {
	case message.UserMessage:
		return typed.Content
	case message.AssistantMessage:
		return typed.Content
	case *message.AssistantMessage:
		return typed.Content
	case message.ToolResultMessage:
		return typed.Content
	}
	return nil
}

// FormatCount writes a token count with thousands separators, as in headings.
func FormatCount(value int) string { return thousands(value) }
