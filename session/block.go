package session

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/niconiahi/the-agent/message"
)

const (
	BLOCK_THINKING    = "thinking"
	BLOCK_TOOL_CALL   = "tool_call"
	BLOCK_TOOL_RESULT = "tool_result"
)

type segment struct {
	kind  string
	attrs map[string]string
	text  string
}

func fence_open(line string) (int, string, bool) {
	trimmed := strings.TrimLeft(line, " ")
	run := len(trimmed) - len(strings.TrimLeft(trimmed, "`"))
	if run < 3 {
		return 0, "", false
	}
	return run, strings.TrimSpace(trimmed[run:]), true
}

func fence_close(line string, run int) bool {
	trimmed := strings.TrimSpace(line)
	return len(trimmed) >= run && strings.Trim(trimmed, "`") == ""
}

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

func repair(body string) string {
	type block struct {
		kind  string
		id    string
		start int
		end   int
	}
	blocks := []block{}
	lines := split_lines(body)
	for index := 0; index < len(lines); index++ {
		run, info, ok := fence_open(strings.TrimSuffix(lines[index], "\n"))
		if !ok {
			continue
		}
		kind, attrs := parse_info(info)
		closing := index + 1
		for closing < len(lines) && !fence_close(strings.TrimSuffix(lines[closing], "\n"), run) {
			closing++
		}
		if kind == BLOCK_TOOL_CALL || kind == BLOCK_TOOL_RESULT {
			blocks = append(blocks, block{kind: kind, id: attrs["id"], start: index, end: min(closing, len(lines)-1)})
		}
		index = closing
	}

	called := map[string]bool{}
	answered := map[string]bool{}
	for _, current := range blocks {
		if current.kind == BLOCK_TOOL_CALL {
			called[current.id] = true
		} else if called[current.id] {
			answered[current.id] = true
		}
	}
	removed := map[int]bool{}
	for _, current := range blocks {
		if answered[current.id] {
			continue
		}
		for line := current.start; line <= current.end; line++ {
			removed[line] = true
		}
		if after := current.end + 1; after < len(lines) && strings.TrimSpace(lines[after]) == "" {
			removed[after] = true
		} else if before := current.start - 1; before >= 0 && strings.TrimSpace(lines[before]) == "" && !removed[before] {
			removed[before] = true
		}
	}
	if len(removed) == 0 {
		return body
	}
	var builder strings.Builder
	for index, line := range lines {
		if !removed[index] {
			builder.WriteString(line)
		}
	}
	return builder.String()
}

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
