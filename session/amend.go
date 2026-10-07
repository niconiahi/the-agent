package session

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/niconiahi/the-agent/message"
)

type fenced struct {
	kind       string
	attributes map[string]string
	start      int
	end        int
}

func fenced_blocks(lines []string) []fenced {
	blocks := []fenced{}
	for index := 0; index < len(lines); index++ {
		run, info, ok := fence_open(strings.TrimSuffix(lines[index], "\n"))
		if !ok {
			continue
		}
		kind, attributes := parse_info(info)
		closing := index + 1
		for closing < len(lines) && !fence_close(strings.TrimSuffix(lines[closing], "\n"), run) {
			closing++
		}
		if closing == len(lines) {
			break
		}
		blocks = append(blocks, fenced{kind: kind, attributes: attributes, start: index, end: closing})
		index = closing
	}
	return blocks
}

type task_pair struct {
	call   message.ToolCall
	result fenced
	found  bool
}

func find_task(lines []string, origin Origin) (task_pair, bool) {
	blocks := fenced_blocks(lines)
	candidates := []task_pair{}
	for index, block := range blocks {
		if block.kind != BLOCK_TOOL_CALL || block.attributes["name"] != "task" || block.attributes["id"] != origin.Call {
			continue
		}
		arguments := map[string]any{}
		body := strings.Join(lines[block.start+1:block.end], "")
		if strings.TrimSpace(body) != "" && json.Unmarshal([]byte(body), &arguments) != nil {
			continue
		}
		pair := task_pair{call: message.ToolCall{ID: origin.Call, Name: "task", Arguments: arguments}}
		for _, next := range blocks[index+1:] {
			if next.attributes["id"] != origin.Call {
				continue
			}
			if next.kind == BLOCK_TOOL_CALL {
				break
			}
			if next.kind == BLOCK_TOOL_RESULT {
				pair.result, pair.found = next, true
				break
			}
		}
		candidates = append(candidates, pair)
	}
	if len(candidates) == 0 {
		return task_pair{}, false
	}
	for index := len(candidates) - 1; index >= 0; index-- {
		if job, _ := candidates[index].call.Arguments["job"].(string); strings.TrimSpace(job) == origin.Job {
			return candidates[index], true
		}
	}
	return candidates[len(candidates)-1], true
}

func TaskCall(text string, origin Origin) (message.ToolCall, bool) {
	pair, ok := find_task(split_lines(text), origin)
	return pair.call, ok
}

func Amend(text string, origin Origin, report string, at time.Time) (string, bool) {
	lines := split_lines(text)
	pair, ok := find_task(lines, origin)
	if !ok || !pair.found {
		return text, false
	}
	info := fmt.Sprintf("%s id=%s amended=%s", BLOCK_TOOL_RESULT, origin.Call, stamp(at))
	amended := render_block(info, report) + "\n"
	return strings.Join(lines[:pair.result.start], "") + amended + strings.Join(lines[pair.result.end+1:], ""), true
}
