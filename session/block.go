package session

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/niconiahi/the-agent/message"
)

// Fenced blocks an assistant turn can hold besides text. Their data lives in
// the info string, e.g. "tool_call id=tc_3 name=edit ts=2026-10-06T14:33:00Z".
const (
	BLOCK_THINKING    = "thinking"
	BLOCK_TOOL_CALL   = "tool_call"
	BLOCK_TOOL_RESULT = "tool_result"
)

// segment is a piece of an assistant turn's body: either free text (kind "")
// or one of the fenced blocks above.
type segment struct {
	kind  string
	attrs map[string]string
	text  string
}

// fence_open reports whether line opens a fenced code block, returning the
// length of its backtick run and its info string.
func fence_open(line string) (int, string, bool) {
	trimmed := strings.TrimLeft(line, " ")
	run := len(trimmed) - len(strings.TrimLeft(trimmed, "`"))
	if run < 3 {
		return 0, "", false
	}
	return run, strings.TrimSpace(trimmed[run:]), true
}

// fence_close reports whether line closes a block opened with run backticks.
func fence_close(line string, run int) bool {
	trimmed := strings.TrimSpace(line)
	return len(trimmed) >= run && strings.Trim(trimmed, "`") == ""
}

// split_lines splits text into lines, each keeping its "\n".
func split_lines(text string) []string {
	lines := strings.SplitAfter(text, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func parse_info(info string) (string, map[string]string) {
	fields := strings.Fields(info)
	if len(fields) == 0 {
		return "", nil
	}
	attrs := map[string]string{}
	for _, field := range fields[1:] {
		if key, value, ok := strings.Cut(field, "="); ok {
			attrs[key] = value
		}
	}
	return fields[0], attrs
}

// segments splits an assistant turn's body into text and blocks. Fences that
// aren't one of ours (```go …) stay part of the text.
func segments(body string) []segment {
	result := []segment{}
	text := ""
	lines := split_lines(body)
	for index := 0; index < len(lines); index++ {
		run, info, ok := fence_open(strings.TrimSuffix(lines[index], "\n"))
		if !ok {
			text += lines[index]
			continue
		}
		kind, attrs := parse_info(info)
		closing := index + 1
		for closing < len(lines) && !fence_close(strings.TrimSuffix(lines[closing], "\n"), run) {
			closing++
		}
		inside := strings.Join(lines[index+1:min(closing, len(lines))], "")
		switch kind {
		case BLOCK_THINKING, BLOCK_TOOL_CALL, BLOCK_TOOL_RESULT:
			result = append(result, segment{text: text})
			text = ""
			result = append(result, segment{kind: kind, attrs: attrs, text: strings.TrimSuffix(inside, "\n")})
		default:
			text += strings.Join(lines[index:min(closing+1, len(lines))], "")
		}
		index = closing
	}
	return append(result, segment{text: text})
}

// assistant_messages turns an assistant turn into the messages it stands
// for: an assistant message per run of text, thinking and tool calls, and a
// tool result message per tool_result block. info (the heading after the
// role) is prepended to the first assistant message's text.
func assistant_messages(info string, body string) ([]message.Message, error) {
	messages := []message.Message{}
	tool_names := map[string]string{}
	var current *message.AssistantMessage

	flush := func() {
		if current == nil {
			return
		}
		current.StopReason = message.STOP_REASON_STOP
		for _, content := range current.Content {
			if _, ok := content.(message.ToolCall); ok {
				current.StopReason = message.STOP_REASON_TOOL_USE
			}
		}
		messages = append(messages, *current)
		current = nil
	}
	add := func(content message.Content) {
		if current == nil {
			current = &message.AssistantMessage{}
		}
		current.Content = append(current.Content, content)
	}

	for _, part := range segments(body) {
		switch part.kind {
		case "":
			if text := strings.TrimSpace(part.text); text != "" {
				add(message.TextContent{Text: text})
			}
		case BLOCK_THINKING:
			add(message.ThinkingContent{Thinking: part.text})
		case BLOCK_TOOL_CALL:
			arguments := map[string]any{}
			if strings.TrimSpace(part.text) != "" {
				if error := json.Unmarshal([]byte(part.text), &arguments); error != nil {
					return nil, fmt.Errorf("tool_call id=%s: arguments are not a JSON object: %w", part.attrs["id"], error)
				}
			}
			tool_names[part.attrs["id"]] = part.attrs["name"]
			add(message.ToolCall{ID: part.attrs["id"], Name: part.attrs["name"], Arguments: arguments})
		case BLOCK_TOOL_RESULT:
			flush()
			text := part.text
			if stamp := part.attrs["ts"]; stamp != "" {
				text = stamp + "\n\n" + text
			}
			messages = append(messages, message.ToolResultMessage{
				ToolCallID: part.attrs["id"],
				ToolName:   tool_names[part.attrs["id"]],
				Content:    []message.Content{message.TextContent{Text: text}},
				IsError:    part.attrs["error"] == "true",
			})
		}
	}
	flush()

	if info != "" {
		prepend_info(messages, info)
	}
	return messages, nil
}

// prepend_info puts info in front of the first assistant message's first
// text, or as its first text when it has none.
func prepend_info(messages []message.Message, info string) {
	for index, value := range messages {
		assistant, ok := value.(message.AssistantMessage)
		if !ok {
			continue
		}
		content := append([]message.Content{}, assistant.Content...)
		for position, part := range content {
			if text, ok := part.(message.TextContent); ok {
				text.Text = info + "\n\n" + text.Text
				content[position] = text
				assistant.Content = content
				messages[index] = assistant
				return
			}
		}
		assistant.Content = append([]message.Content{message.TextContent{Text: info}}, content...)
		messages[index] = assistant
		return
	}
}

// render_block writes a fenced block whose fence is longer than any run of
// backticks in body, so the body can't close it early.
func render_block(info string, body string) string {
	run := 3
	for _, field := range strings.FieldsFunc(body, func(r rune) bool { return r != '`' }) {
		run = max(run, len(field)+1)
	}
	fence := strings.Repeat("`", run)
	if body == "" {
		return fence + info + "\n" + fence
	}
	return fence + info + "\n" + body + "\n" + fence
}

func render_thinking(thinking string) string {
	return render_block(BLOCK_THINKING, thinking)
}

func render_tool_call(call message.ToolCall, at time.Time) (string, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	arguments := call.Arguments
	if arguments == nil {
		arguments = map[string]any{}
	}
	if error := encoder.Encode(arguments); error != nil {
		return "", fmt.Errorf("tool_call id=%s: %w", call.ID, error)
	}
	info := fmt.Sprintf("%s id=%s name=%s ts=%s", BLOCK_TOOL_CALL, call.ID, call.Name, stamp(at))
	return render_block(info, strings.TrimSuffix(buffer.String(), "\n")), nil
}

func render_tool_result(result message.ToolResultMessage, at time.Time) string {
	info := fmt.Sprintf("%s id=%s ts=%s", BLOCK_TOOL_RESULT, result.ToolCallID, stamp(at))
	if result.IsError {
		info += " error=true"
	}
	parts := []string{}
	for _, content := range result.Content {
		if text, ok := content.(message.TextContent); ok {
			parts = append(parts, text.Text)
		}
	}
	return render_block(info, strings.Join(parts, "\n"))
}

func stamp(at time.Time) string {
	return at.UTC().Format(TIMESTAMP_FORMAT)
}
