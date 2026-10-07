package integration_test

import (
	"path/filepath"
	"strings"
	"testing"

	neovim "github.com/neovim/go-client/nvim"

	"github.com/niconiahi/the-agent/message"
	"github.com/niconiahi/the-agent/nvim"
	"github.com/niconiahi/the-agent/nvim/nvimtest"
	"github.com/niconiahi/the-agent/tool"
	"github.com/niconiahi/the-agent/vimtool"
)

func vimtool_tools(client *neovim.Nvim) []tool.Tool {
	return []tool.Tool{vimtool.Read(client), vimtool.Edit(client), vimtool.Write(client), vimtool.Filter(client), vimtool.Grep(client)}
}

func start_with_vimtool(t *testing.T, calls ...message.ToolCall) (*nvimtest.Harness, *nvimtest.Provider) {
	t.Helper()
	return start_with_vimtool_config(t, nvimtest.Config(), calls...)
}

func start_with_vimtool_config(t *testing.T, config nvim.Config, calls ...message.ToolCall) (*nvimtest.Harness, *nvimtest.Provider) {
	t.Helper()
	replies := []nvimtest.Reply{}
	for _, call := range calls {
		replies = append(replies, nvimtest.Reply{ToolCalls: []message.ToolCall{call}})
	}
	replies = append(replies, nvimtest.Text("done", 10))
	provider := nvimtest.RegisterProvider(t, replies...)
	harness := nvimtest.StartWithTools(t, config, vimtool_tools)
	return harness, provider
}

func call(id string, name string, arguments map[string]any) message.ToolCall {
	return message.ToolCall{ID: id, Name: name, Arguments: arguments}
}

func send_agent_turn(harness *nvimtest.Harness) {
	harness.T.Helper()
	harness.Command("TA foo")
	harness.SetText(harness.Text() + "go\n")
	harness.Command("TASend")
	harness.WaitFor("the turn", func() bool { return strings.HasSuffix(on_disk(harness), "\ndone\n\n## user\n\n") })
}

func tool_results(t *testing.T, provider *nvimtest.Provider) []message.ToolResultMessage {
	t.Helper()
	requests := provider.Requests()
	results := []message.ToolResultMessage{}
	for _, value := range requests[len(requests)-1].Messages {
		if result, ok := value.(message.ToolResultMessage); ok {
			results = append(results, result)
		}
	}
	return results
}

func result_text(result message.ToolResultMessage) string {
	return result.Content[0].(message.TextContent).Text
}

func buffer_lines(harness *nvimtest.Harness, path string) string {
	harness.T.Helper()
	var lines []string
	if error := harness.Nvim.ExecLua(`return vim.api.nvim_buf_get_lines(vim.fn.bufnr(...), 0, -1, true)`, &lines, path); error != nil {
		harness.T.Fatal(error)
	}
	return strings.Join(lines, "\n") + "\n"
}

func agent_tick(harness *nvimtest.Harness, path string) int {
	harness.T.Helper()
	var tick int
	agent := filepath.Dir(filepath.Join(harness.Dir, SESSION))
	if error := harness.Nvim.ExecLua(`local path, agent = ...; local ticks = vim.b[vim.fn.bufnr(path)].the_agent_ticks or {}; return ticks[agent] or -1`, &tick, path, agent); error != nil {
		harness.T.Fatal(error)
	}
	return tick
}

func buffer_tick(harness *nvimtest.Harness, path string) int {
	harness.T.Helper()
	var tick int
	if error := harness.Nvim.ExecLua(`return vim.api.nvim_buf_get_changedtick(vim.fn.bufnr(...))`, &tick, path); error != nil {
		harness.T.Fatal(error)
	}
	return tick
}

func undo(harness *nvimtest.Harness, path string) {
	harness.T.Helper()
	if error := harness.Nvim.ExecLua(`vim.api.nvim_buf_call(vim.fn.bufnr(...), function() vim.cmd("silent undo") end)`, nil, path); error != nil {
		harness.T.Fatal(error)
	}
}

func record_notifications(harness *nvimtest.Harness) func() []string {
	harness.T.Helper()
	harness.Command(`lua _G.notes = {}; vim.notify = function(message) table.insert(_G.notes, message) end`)
	return func() []string {
		var notes []string
		if error := harness.Nvim.ExecLua(`return _G.notes`, &notes); error != nil {
			harness.T.Fatal(error)
		}
		return notes
	}
}
