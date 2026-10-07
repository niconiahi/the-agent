package integration_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/niconiahi/the-agent/message"
	"github.com/niconiahi/the-agent/nvim"
	"github.com/niconiahi/the-agent/nvim/nvimtest"
	"github.com/niconiahi/the-agent/vimtool"
)

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
	harness := nvimtest.StartWithTools(t, config, vimtool.Tools)
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

func TestRead_CleanOpenBufferReturnsItsTextAndRecordsTheTick(t *testing.T) {
	harness, provider := start_with_vimtool(t, call("tc_1", "read", map[string]any{"path": "a.txt"}))
	path := filepath.Join(harness.Dir, "a.txt")
	harness.WriteFile("a.txt", "one\ntwo\n")
	harness.Command("edit " + path)

	send_agent_turn(harness)

	results := tool_results(t, provider)
	if got := result_text(results[0]); got != "1\tone\n2\ttwo\n3\t\n" || results[0].IsError {
		t.Fatalf("read result: %q", got)
	}
	if got, want := agent_tick(harness, path), buffer_tick(harness, path); got != want {
		t.Fatalf("recorded tick %d, buffer tick %d", got, want)
	}
}

func TestRead_BufferWithMyUnsavedChangesReturnsTheDisk(t *testing.T) {
	harness, provider := start_with_vimtool(t, call("tc_1", "read", map[string]any{"path": "a.txt"}))
	path := filepath.Join(harness.Dir, "a.txt")
	harness.WriteFile("a.txt", "one\n")
	harness.Command("edit " + path)
	harness.SetText("half finished\n")

	send_agent_turn(harness)

	results := tool_results(t, provider)
	if got := result_text(results[0]); got != "1\tone\n2\t\n" {
		t.Fatalf("read result: %q", got)
	}
	if got := buffer_lines(harness, path); got != "half finished\n" {
		t.Fatalf("buffer: %q", got)
	}
	if got, want := agent_tick(harness, path), buffer_tick(harness, path); got != want {
		t.Fatalf("recorded tick %d, buffer tick %d", got, want)
	}
}

func TestRead_FileThatIsNotOpenReturnsTheDisk(t *testing.T) {
	harness, provider := start_with_vimtool(t,
		call("tc_1", "read", map[string]any{"path": "a.txt"}),
		call("tc_2", "read", map[string]any{"path": "missing.txt"}))
	harness.WriteFile("a.txt", "one\n")

	send_agent_turn(harness)

	results := tool_results(t, provider)
	if got := result_text(results[0]); got != "1\tone\n2\t\n" {
		t.Fatalf("read result: %q", got)
	}
	if !results[1].IsError || !strings.Contains(result_text(results[1]), "failed to read file") {
		t.Fatalf("missing file: %#v", results[1])
	}
}
