package session

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/niconiahi/the-agent/message"
)

const (
	ROLE_USER      = "user"
	ROLE_ASSISTANT = "assistant"
)

const TIMESTAMP_FORMAT = time.RFC3339

type Session struct {
	preamble string
	turns    []turn
}

type turn struct {
	role    string
	heading string
	body    string
}

func Parse(text string) (*Session, error) {
	parsed := &Session{}
	fence := 0
	offset := 0

	for offset < len(text) {
		end := strings.IndexByte(text[offset:], '\n')
		line_end := len(text)
		next := len(text)
		if end >= 0 {
			line_end = offset + end
			next = line_end + 1
		}
		line := text[offset:line_end]

		in_fence := fence > 0
		if in_fence {
			if fence_close(line, fence) {
				fence = 0
			}
		} else if run, _, ok := fence_open(line); ok {
			fence = run
		}

		chunk := text[offset:next]
		if role, ok := heading_role(line); ok && !in_fence {
			parsed.turns = append(parsed.turns, turn{role: role, heading: line})

			chunk = text[line_end:next]
		}

		if len(parsed.turns) == 0 {
			parsed.preamble += chunk
		} else {
			parsed.turns[len(parsed.turns)-1].body += chunk
		}
		offset = next
	}

	for _, current := range parsed.turns {
		if current.role != ROLE_ASSISTANT {
			continue
		}
		if _, error := assistant_messages(heading_info(current), current.body); error != nil {
			return nil, fmt.Errorf("%s: %w", current.heading, error)
		}
	}
	return parsed, nil
}

func (parsed *Session) Repair() bool {
	changed := false
	for index := range parsed.turns {
		current := &parsed.turns[index]
		if current.role != ROLE_ASSISTANT {
			continue
		}
		if repaired := repair(current.body); repaired != current.body {
			current.body = repaired
			changed = true
		}
	}
	return changed
}

func heading_role(line string) (string, bool) {
	rest, ok := strings.CutPrefix(line, "## ")
	if !ok {
		return "", false
	}
	role := rest
	if index := strings.IndexAny(rest, " ·"); index >= 0 {
		role = rest[:index]
	}
	switch role {
	case ROLE_USER, ROLE_ASSISTANT:
		return role, true
	}
	return "", false
}

func (parsed *Session) Render() string {
	var builder strings.Builder
	builder.WriteString(parsed.preamble)
	for _, current := range parsed.turns {
		builder.WriteString(current.heading)
		builder.WriteString(current.body)
	}
	return builder.String()
}

func (parsed *Session) Messages() []message.Message {
	messages := []message.Message{}
	for _, current := range parsed.turns {
		if current.role == ROLE_ASSISTANT {
			current.body = repair(current.body)
		}
		body := strings.TrimSpace(current.body)
		if body == "" {
			continue
		}
		info := heading_info(current)

		switch current.role {
		case ROLE_USER:
			text := body
			if info != "" {
				text = info + "\n\n" + body
			}
			messages = append(messages, message.UserMessage{Content: []message.Content{message.TextContent{Text: text}}})
		case ROLE_ASSISTANT:

			converted, _ := assistant_messages(info, current.body)
			messages = append(messages, converted...)
		}
	}
	return messages
}

func heading_info(current turn) string {
	rest := strings.TrimPrefix(current.heading, "## "+current.role)
	return strings.TrimSpace(strings.TrimLeft(rest, " ·"))
}

func (parsed *Session) StampLastUser(now time.Time) bool {
	if len(parsed.turns) == 0 {
		return false
	}
	last := &parsed.turns[len(parsed.turns)-1]
	if last.role != ROLE_USER || strings.TrimSpace(last.heading) != "## user" {
		return false
	}
	last.heading = "## user · " + now.UTC().Format(TIMESTAMP_FORMAT)
	return true
}

func (parsed *Session) AppendAssistant(model string, at time.Time, tokens int, text string) {
	parsed.AppendAssistantMessage(model, at, message.AssistantMessage{
		Content: []message.Content{message.TextContent{Text: text}},
		Usage:   message.Usage{TotalTokens: tokens},
	})
}

func (parsed *Session) AppendAssistantMessage(model string, at time.Time, reply message.AssistantMessage) error {
	blocks := []string{}
	for _, content := range reply.Content {
		switch typed := content.(type) {
		case message.TextContent:
			if text := strings.TrimSpace(typed.Text); text != "" {
				blocks = append(blocks, text)
			}
		case message.ThinkingContent:
			if strings.TrimSpace(typed.Thinking) != "" {
				blocks = append(blocks, render_thinking(typed.Thinking))
			}
		case message.ToolCall:
			block, error := render_tool_call(typed, at)
			if error != nil {
				return error
			}
			blocks = append(blocks, block)
		}
	}
	parsed.separate()
	parsed.turns = append(parsed.turns, turn{
		role:    ROLE_ASSISTANT,
		heading: fmt.Sprintf("## assistant · %s · %s · %s tokens", model, stamp(at), FormatCount(reply.Usage.TotalTokens)),
		body:    "\n\n" + strings.Join(blocks, "\n\n") + "\n",
	})
	return nil
}

func (parsed *Session) AppendToolResult(result message.ToolResultMessage, at time.Time, directory string) error {
	block, error := render_tool_result(result, at, directory)
	if error != nil {
		return error
	}
	parsed.separate()
	block += "\n"
	if len(parsed.turns) == 0 {
		parsed.preamble += block
		return nil
	}
	parsed.turns[len(parsed.turns)-1].body += block
	return nil
}

func (parsed *Session) AppendUser() {
	parsed.separate()
	parsed.turns = append(parsed.turns, turn{role: ROLE_USER, heading: "## user", body: "\n\n"})
}

func (parsed *Session) separate() {
	tail := &parsed.preamble
	if len(parsed.turns) > 0 {
		tail = &parsed.turns[len(parsed.turns)-1].body
	}
	rendered := parsed.Render()
	if rendered == "" {
		return
	}
	if !strings.HasSuffix(rendered, "\n") {
		*tail += "\n"
		rendered += "\n"
	}
	if !strings.HasSuffix(rendered, "\n\n") {
		*tail += "\n"
	}
}

func FormatTokens(value int) string {
	return FormatCount(value) + " tokens"
}

func FormatCount(value int) string {
	digits := strconv.Itoa(value)
	if value < 0 {
		return "-" + FormatCount(-value)
	}
	var builder strings.Builder
	for index, digit := range digits {
		if index > 0 && (len(digits)-index)%3 == 0 {
			builder.WriteByte(',')
		}
		builder.WriteRune(digit)
	}
	return builder.String()
}
