package integration_test

import (
	"path/filepath"
	"testing"

	"github.com/niconiahi/the-agent/message"
	"github.com/niconiahi/the-agent/nvim/nvimtest"
)

const STALE = "file changed since you read it, re-read first"

// A turn whose replies are the given tool calls, one per reply, then
// "done"; the edit at gated holds its call until the test steps it, so the
// test can change the buffer after the tool calls before it have run.
func start_turn_held_at(t *testing.T, gated int, calls ...message.ToolCall) (*nvimtest.Harness, *nvimtest.Provider, *nvimtest.Gate) {
	t.Helper()
	gate := nvimtest.NewGate()
	replies := []nvimtest.Reply{}
	for index, current := range calls {
		reply := nvimtest.Reply{ToolCalls: []message.ToolCall{current}}
		if index == gated {
			reply.Gate = gate
		}
		replies = append(replies, reply)
	}
	replies = append(replies, nvimtest.Text("done", 10))
	provider := nvimtest.RegisterProvider(t, replies...)
	harness := nvimtest.StartWithTools(t, nvimtest.Config(), vimtool_tools)
	return harness, provider, gate
}

func send_held_turn(harness *nvimtest.Harness, gate *nvimtest.Gate, between func()) {
	harness.T.Helper()
	harness.Command("TA foo")
	harness.SetText(harness.Text() + "go\n")
	harness.Command("TASend")
	gate.Step(harness.T)
	between()
	gate.Step(harness.T)
	wait_for_session_end(harness, "foo", "done")
}

func type_into(harness *nvimtest.Harness, path string, keys string) {
	harness.T.Helper()
	code := `local path, keys = ...; vim.api.nvim_buf_call(vim.fn.bufnr(path), function() vim.cmd("normal! " .. keys) end)`
	if error := harness.Nvim.ExecLua(code, nil, path, keys); error != nil {
		harness.T.Fatal(error)
	}
}

func TestEdit_RejectsWhenITypedInTheBufferAfterTheAgentRead(t *testing.T) {
	harness, provider, gate := start_turn_held_at(t, 1,
		call("tc_1", "read", map[string]any{"path": "a.txt"}),
		call("tc_2", "edit", map[string]any{"path": "a.txt", "old_text": "two", "new_text": "TWO"}))
	path := filepath.Join(harness.Dir, "a.txt")
	harness.WriteFile("a.txt", "one\ntwo\n")
	harness.Command("edit " + path)

	send_held_turn(harness, gate, func() { type_into(harness, path, "ggAx") })

	results := tool_results(t, provider)
	if !results[1].IsError || result_text(results[1]) != STALE {
		t.Fatalf("edit result: %#v", results[1])
	}
	if got := buffer_lines(harness, path); got != "onex\ntwo\n" {
		t.Fatalf("buffer: %q", got)
	}
	if got := harness.ReadFile("a.txt"); got != "one\ntwo\n" {
		t.Fatalf("disk: %q", got)
	}
}

func TestEdit_ConsecutiveEditsByTheSameAgentAfterOneReadSucceed(t *testing.T) {
	harness, provider := start_with_vimtool(t,
		call("tc_1", "read", map[string]any{"path": "a.txt"}),
		call("tc_2", "edit", map[string]any{"path": "a.txt", "old_text": "one", "new_text": "ONE"}),
		call("tc_3", "edit", map[string]any{"path": "a.txt", "old_text": "two", "new_text": "TWO"}))
	path := filepath.Join(harness.Dir, "a.txt")
	harness.WriteFile("a.txt", "one\ntwo\n")
	harness.Command("edit " + path)

	send_agent_turn(harness)

	for _, result := range tool_results(t, provider)[1:] {
		if result.IsError {
			t.Fatalf("edit result: %#v", result)
		}
	}
	if got := harness.ReadFile("a.txt"); got != "ONE\nTWO\n" {
		t.Fatalf("disk: %q", got)
	}
}

func TestEdit_SucceedsAfterTheAgentReReadsWhatITyped(t *testing.T) {
	harness, provider, gate := start_turn_held_at(t, 1,
		call("tc_1", "read", map[string]any{"path": "a.txt"}),
		call("tc_2", "read", map[string]any{"path": "a.txt"}),
		call("tc_3", "edit", map[string]any{"path": "a.txt", "old_text": "two", "new_text": "TWO"}))
	path := filepath.Join(harness.Dir, "a.txt")
	harness.WriteFile("a.txt", "one\ntwo\n")
	harness.Command("edit " + path)

	send_held_turn(harness, gate, func() {
		type_into(harness, path, "ggAx")
		harness.Command("wall")
	})

	results := tool_results(t, provider)
	if got := result_text(results[1]); got != "1\tonex\n2\ttwo\n3\t\n" {
		t.Fatalf("re-read result: %q", got)
	}
	if results[2].IsError {
		t.Fatalf("edit result: %#v", results[2])
	}
	if got := harness.ReadFile("a.txt"); got != "onex\nTWO\n" {
		t.Fatalf("disk: %q", got)
	}
}

func result_of(t *testing.T, provider *nvimtest.Provider, id string) message.ToolResultMessage {
	t.Helper()
	for _, request := range provider.Requests() {
		for _, value := range request.Messages {
			if result, ok := value.(message.ToolResultMessage); ok && result.ToolCallID == id {
				return result
			}
		}
	}
	t.Fatalf("no result for tool call %s", id)
	return message.ToolResultMessage{}
}

func TestEdit_RejectsWhenAnotherAgentEditedTheFileAfterTheAgentRead(t *testing.T) {
	held := nvimtest.Reply{
		ToolCalls: []message.ToolCall{call("alpha_2", "edit", map[string]any{"path": "a.txt", "old_text": "two", "new_text": "TWO"})},
		Gate:      nvimtest.NewGate(),
	}
	provider := nvimtest.RegisterProvider(t)
	provider.Script("alpha",
		nvimtest.Reply{ToolCalls: []message.ToolCall{call("alpha_1", "read", map[string]any{"path": "a.txt"})}},
		held,
		nvimtest.Text("done", 10))
	provider.Script("beta",
		nvimtest.Reply{ToolCalls: []message.ToolCall{call("beta_1", "edit", map[string]any{"path": "a.txt", "old_text": "one", "new_text": "ONE"})}},
		nvimtest.Text("done", 10))
	harness := nvimtest.StartWithTools(t, nvimtest.Config(), vimtool_tools)
	path := filepath.Join(harness.Dir, "a.txt")
	harness.WriteFile("a.txt", "one\ntwo\n")
	harness.Command("edit " + path)

	send_in(harness, "one", "alpha")
	held.Gate.Step(t)
	send_in(harness, "two", "beta")
	wait_for_session_end(harness, "two", "done")
	held.Gate.Step(t)
	wait_for_session_end(harness, "one", "done")

	if result := result_of(t, provider, "beta_1"); result.IsError {
		t.Fatalf("beta's edit: %#v", result)
	}
	if result := result_of(t, provider, "alpha_2"); !result.IsError || result_text(result) != STALE {
		t.Fatalf("alpha's edit: %#v", result)
	}
	if got := harness.ReadFile("a.txt"); got != "ONE\ntwo\n" {
		t.Fatalf("disk: %q", got)
	}
}
