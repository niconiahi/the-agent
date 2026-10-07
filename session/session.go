// Package session owns the session.md format: it parses a session file into
// the messages the model receives and renders it back, losslessly.
//
// A turn starts at a "## user" or "## assistant" heading outside a fenced
// block. Only the role is read from the heading; the rest of the heading line
// is free-form, kept verbatim, and sent to the model as the first line of the
// message so the timestamps in the file are part of the context.
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

// TIMESTAMP_FORMAT is the ISO 8601 layout used for every timestamp written
// into a session file.
const TIMESTAMP_FORMAT = time.RFC3339

type Session struct {
	preamble string
	turns    []turn
}

type turn struct {
	role    string
	heading string // the heading line, without its line terminator
	body    string // everything after the heading text up to the next turn
}

func Parse(text string) (*Session, error) {
	parsed := &Session{}
	fence := 0 // backtick run of the open fenced block, 0 outside one
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
			// The newline that ends the heading belongs to the body.
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

// Repair removes from the file every orphaned half of a tool pair: a
// tool_call with no tool_result of the same id after it in its turn, or a
// tool_result with no such tool_call before it. Messages never sends them
// anyway, since providers reject a broken pair; Repair makes the file show
// exactly what was sent. Thinking is never touched. It reports whether
// anything was removed.
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

// Messages returns what the model receives. A user turn becomes one message
// whose text is the free-form part of its heading (timestamps included)
// followed by the body. An assistant turn becomes its assistant message,
// with thinking and tool_call blocks as content, followed by a tool result
// message per tool_result block (see assistant_messages), leaving out the
// orphans Repair removes. Turns with nothing to say are skipped.
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
			// Parse already rejected turns that don't convert.
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

// StampLastUser adds a timestamp to the last turn's heading when it is a bare
// "## user" heading. It reports whether the heading changed.
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

// AppendAssistant adds an assistant turn with its model, timestamp and token
// count in the heading.
func (parsed *Session) AppendAssistant(model string, at time.Time, tokens int, text string) {
	parsed.AppendAssistantMessage(model, at, message.AssistantMessage{
		Content: []message.Content{message.TextContent{Text: text}},
		Usage:   message.Usage{TotalTokens: tokens},
	})
}

// AppendAssistantMessage adds an assistant turn for reply: its heading
// carries model, at and the reply's total tokens, and its body holds the
// reply's content in order — thinking and tool_call blocks (stamped with at)
// and text.
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
		heading: fmt.Sprintf("## assistant · %s · %s · %s tokens", model, stamp(at), thousands(reply.Usage.TotalTokens)),
		body:    "\n\n" + strings.Join(blocks, "\n\n") + "\n",
	})
	return nil
}

// AppendToolResult adds result as a tool_result block, stamped with at, to
// the end of the last turn (the assistant turn that called the tool).
func (parsed *Session) AppendToolResult(result message.ToolResultMessage, at time.Time) {
	parsed.separate()
	block := render_tool_result(result, at) + "\n"
	if len(parsed.turns) == 0 {
		parsed.preamble += block
		return
	}
	parsed.turns[len(parsed.turns)-1].body += block
}

// AppendUser adds a bare "## user" heading for the next message; it is
// stamped when that message is sent.
func (parsed *Session) AppendUser() {
	parsed.separate()
	parsed.turns = append(parsed.turns, turn{role: ROLE_USER, heading: "## user", body: "\n\n"})
}

// separate makes the file end in a blank line so the next heading stands apart.
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

func thousands(value int) string {
	digits := strconv.Itoa(value)
	if value < 0 {
		return "-" + thousands(-value)
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
