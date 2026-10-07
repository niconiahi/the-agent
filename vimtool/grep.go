package vimtool

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	neovim "github.com/neovim/go-client/nvim"

	"github.com/niconiahi/the-agent/message"
	"github.com/niconiahi/the-agent/tool"
)

const QUICKFIX_TITLE = "the-agent grep"

var HIT_IN_DIRECTORY = regexp.MustCompile(`^(.+?):(\d+):(.*)$`)

var HIT_IN_FILE = regexp.MustCompile(`^(\d+):(.*)$`)

type quickfix_item struct {
	Filename string `msgpack:"filename"`
	Line     int    `msgpack:"lnum"`
	Text     string `msgpack:"text"`
}

func Grep(client *neovim.Nvim) tool.Tool {
	grep := tool.GrepTool()
	search := grep.Execute
	grep.Execute = func(invocation_context context.Context, tool_call_id string, arguments map[string]any) (tool.ToolResult, error) {
		result, error := search(invocation_context, tool_call_id, arguments)
		if error != nil {
			return result, error
		}
		items := hits(search_path(arguments), result)
		if error := client.ExecLua(`vim.fn.setqflist({}, " ", { title = select(1, ...), items = select(2, ...) })`, nil, QUICKFIX_TITLE, items); error != nil {
			log.Printf("the-agent: grep quickfix: %v", error)
		}
		return result, nil
	}
	return grep
}

func search_path(arguments map[string]any) string {
	if path, ok := arguments["path"].(string); ok {
		return path
	}
	return "."
}

func hits(path string, result tool.ToolResult) []quickfix_item {
	items := []quickfix_item{}
	if len(result.Content) == 0 {
		return items
	}
	text, ok := result.Content[0].(message.TextContent)
	if !ok {
		return items
	}
	information, error := os.Stat(path)
	single_file := error == nil && !information.IsDir()
	for _, line := range strings.Split(text.Text, "\n") {
		filename, number, hit := path, "", ""
		if single_file {
			match := HIT_IN_FILE.FindStringSubmatch(line)
			if match == nil {
				continue
			}
			number, hit = match[1], match[2]
		} else {
			match := HIT_IN_DIRECTORY.FindStringSubmatch(line)
			if match == nil {
				continue
			}
			filename, number, hit = match[1], match[2], match[3]
		}
		absolute, error := filepath.Abs(filename)
		if error != nil {
			continue
		}
		line_number, _ := strconv.Atoi(number)
		items = append(items, quickfix_item{Filename: absolute, Line: line_number, Text: hit})
	}
	return items
}
