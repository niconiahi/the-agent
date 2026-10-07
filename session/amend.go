package session

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/niconiahi/the-agent/message"
)

// fenced is a tool block found in a session's text, by line: start is its
// opening fence and end its closing one.
type fenced struct {
	kind  string
	attrs map[string]string
	start int
	end   int
}

func fenced_blocks(lines []string) []fenced {
	blocks := []fenced{}
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
		if closing == len(lines) {
			break
		}
		blocks = append(blocks, fenced{kind: kind, attrs: attrs, start: index, end: closing})
		index = closing
	}
	return blocks
}

// linked_result finds the tool_result right after the line that is link,
// with only blank lines between: the result a subagent's report went into.
func linked_result(lines []string, link string) (fenced, bool) {
	blocks := fenced_blocks(lines)
	for index, line := range lines {
		if strings.TrimSpace(line) != link {
			continue
		}
		next := index + 1
		for next < len(lines) && strings.TrimSpace(lines[next]) == "" {
			next++
		}
		for _, block := range blocks {
			if block.start == next && block.kind == BLOCK_TOOL_RESULT {
				return block, true
			}
		}
	}
	return fenced{}, false
}

// LinkedCall is the tool call whose result follows link in text, the call
// that started the subagent link points to. It is false when that result
// or its call is gone.
func LinkedCall(text string, link string) (message.ToolCall, bool) {
	lines := split_lines(text)
	result, ok := linked_result(lines, link)
	if !ok {
		return message.ToolCall{}, false
	}
	for _, block := range fenced_blocks(lines) {
		if block.kind != BLOCK_TOOL_CALL || block.attrs["id"] != result.attrs["id"] {
			continue
		}
		arguments := map[string]any{}
		body := strings.Join(lines[block.start+1:block.end], "")
		if strings.TrimSpace(body) != "" && json.Unmarshal([]byte(body), &arguments) != nil {
			return message.ToolCall{}, false
		}
		return message.ToolCall{ID: block.attrs["id"], Name: block.attrs["name"], Arguments: arguments}, true
	}
	return message.ToolCall{}, false
}

// Amend replaces the tool_result that follows link in text with report,
// marked amended=at: a continued subagent's new final answer. It returns
// text unchanged, and false, when there is no such result.
func Amend(text string, link string, report string, at time.Time) (string, bool) {
	lines := split_lines(text)
	result, ok := linked_result(lines, link)
	if !ok {
		return text, false
	}
	info := fmt.Sprintf("%s id=%s amended=%s", BLOCK_TOOL_RESULT, result.attrs["id"], stamp(at))
	amended := render_block(info, report) + "\n"
	return strings.Join(lines[:result.start], "") + amended + strings.Join(lines[result.end+1:], ""), true
}
