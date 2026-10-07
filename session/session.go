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
	in_fence := false
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

		if strings.HasPrefix(strings.TrimLeft(line, " "), "```") {
			in_fence = !in_fence
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

	return parsed, nil
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

// Messages returns what the model receives. Each turn becomes one message
// whose text is the free-form part of its heading (timestamps included)
// followed by the body. Turns with nothing to say are skipped.
func (parsed *Session) Messages() []message.Message {
	messages := []message.Message{}
	for _, current := range parsed.turns {
		body := strings.TrimSpace(current.body)
		if body == "" {
			continue
		}
		text := body
		if info := heading_info(current); info != "" {
			text = info + "\n\n" + body
		}
		content := []message.Content{message.TextContent{Text: text}}

		switch current.role {
		case ROLE_USER:
			messages = append(messages, message.UserMessage{Content: content})
		case ROLE_ASSISTANT:
			messages = append(messages, message.AssistantMessage{
				Content:    content,
				StopReason: message.STOP_REASON_STOP,
			})
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
	parsed.separate()
	parsed.turns = append(parsed.turns, turn{
		role:    ROLE_ASSISTANT,
		heading: fmt.Sprintf("## assistant · %s · %s · %s tokens", model, at.UTC().Format(TIMESTAMP_FORMAT), thousands(tokens)),
		body:    "\n\n" + strings.TrimSpace(text) + "\n",
	})
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

// FormatTokens is a token count as written in a heading: "1,240 tokens".
func FormatTokens(value int) string {
	return thousands(value) + " tokens"
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
