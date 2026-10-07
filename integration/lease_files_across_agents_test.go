package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	neovim "github.com/neovim/go-client/nvim"

	"github.com/niconiahi/the-agent/message"
	"github.com/niconiahi/the-agent/nvim"
	"github.com/niconiahi/the-agent/nvim/nvimtest"
	"github.com/niconiahi/the-agent/tool"
)

const LEASE_ADVICE = "; work on other files or finish without it"

func edit_call(id string, path string, old_text string, new_text string) message.ToolCall {
	return call(id, "edit", map[string]any{"path": path, "old_text": old_text, "new_text": new_text})
}

func child_folder(harness *nvimtest.Harness, job string) string {
	harness.T.Helper()
	matches, _ := filepath.Glob(filepath.Join(filepath.Dir(nvim.SessionPath(harness.Dir, "foo")), "*-"+job))
	if len(matches) != 1 {
		harness.T.Fatalf("child folders for %q: %v", job, matches)
	}
	return filepath.Base(matches[0])
}

func child_on_disk(harness *nvimtest.Harness, folder string) string {
	contents, _ := os.ReadFile(child_session(harness, folder))
	return string(contents)
}

func TestLease_AWorkerEditingAFileAnotherWorkerLeasedFailsAtOnceNamingTheHolder(t *testing.T) {
	provider := nvimtest.RegisterProvider(t,
		nvimtest.Reply{ToolCalls: []message.ToolCall{
			call("t1", "task", map[string]any{"role": "worker", "job": "first job"}),
			call("t2", "task", map[string]any{"role": "worker", "job": "second job"}),
		}},
		nvimtest.Text("done", 10),
	)
	first_held := nvimtest.Reply{ToolCalls: []message.ToolCall{call("f2", "read", map[string]any{"path": "a.txt"})}, Gate: nvimtest.NewGate()}
	provider.Script("first job",
		nvimtest.Reply{ToolCalls: []message.ToolCall{edit_call("f1", "a.txt", "one", "ONE")}},
		first_held,
		nvimtest.Text("first report", 1),
	)
	second_held := nvimtest.Reply{ToolCalls: []message.ToolCall{edit_call("s1", "a.txt", "two", "TWO")}, Gate: nvimtest.NewGate()}
	second_after := nvimtest.Reply{ToolCalls: []message.ToolCall{edit_call("s2", "a.txt", "two", "TWO")}, Gate: nvimtest.NewGate()}
	provider.Script("second job", second_held, second_after, nvimtest.Text("second report", 1))
	harness := nvimtest.StartWithTools(t, nvimtest.Config(), vimtool_tools)
	harness.WriteFile("a.txt", "one\ntwo\n")
	harness.Command("TA foo")
	harness.SetText(harness.Text() + "go\n")
	harness.Command("TASend")

	harness.WaitFor("the first worker's edit", func() bool { return harness.ReadFile("a.txt") == "ONE\ntwo\n" })
	second_held.Gate.Step(t)
	second_held.Gate.Step(t)
	harness.WaitFor("the second worker's next request", func() bool { return len(request_of(provider, "second job")) == 2 })

	refused := result_of(t, provider, "s1")
	want := "a.txt is being edited by subagent foo/" + child_folder(harness, "first-job") + LEASE_ADVICE
	if !refused.IsError || result_text(refused) != want {
		t.Fatalf("second worker's edit: got %q (error %v), want %q", result_text(refused), refused.IsError, want)
	}
	if got := harness.ReadFile("a.txt"); got != "ONE\ntwo\n" {
		t.Fatalf("disk after the refused edit: %q", got)
	}

	first_held.Gate.Step(t)
	first_held.Gate.Step(t)
	first := child_folder(harness, "first-job")
	harness.WaitFor("the first worker's end", func() bool {
		return strings.HasSuffix(child_on_disk(harness, first), "\nfirst report\n\n## user\n\n")
	})
	second_after.Gate.Step(t)
	second_after.Gate.Step(t)
	wait_for_parent(harness)

	if result := result_of(t, provider, "s2"); result.IsError {
		t.Fatalf("the second worker's edit after the first ended: %q", result_text(result))
	}
	if got := harness.ReadFile("a.txt"); got != "ONE\nTWO\n" {
		t.Fatalf("disk: %q", got)
	}
}

func path_modifiable(harness *nvimtest.Harness, path string) bool {
	harness.T.Helper()
	var value bool
	if error := harness.Nvim.ExecLua(`return vim.bo[vim.fn.bufnr(...)].modifiable`, &value, path); error != nil {
		harness.T.Fatal(error)
	}
	return value
}

func TestLease_ICannotTypeIntoALeasedBufferButCanIntoOneTheAgentOnlyRead(t *testing.T) {
	held := nvimtest.Reply{ToolCalls: []message.ToolCall{call("tc_3", "read", map[string]any{"path": "b.txt"})}, Gate: nvimtest.NewGate()}
	nvimtest.RegisterProvider(t,
		nvimtest.Reply{ToolCalls: []message.ToolCall{call("tc_1", "read", map[string]any{"path": "b.txt"})}},
		nvimtest.Reply{ToolCalls: []message.ToolCall{edit_call("tc_2", "a.txt", "one", "ONE")}},
		held,
		nvimtest.Text("done", 10),
	)
	harness := nvimtest.StartWithTools(t, nvimtest.Config(), vimtool_tools)
	edited, read := filepath.Join(harness.Dir, "a.txt"), filepath.Join(harness.Dir, "b.txt")
	harness.WriteFile("a.txt", "one\n")
	harness.WriteFile("b.txt", "bee\n")
	harness.Command("edit " + edited)
	harness.Command("edit " + read)
	harness.Command("TA foo")
	harness.SetText(harness.Text() + "go\n")
	harness.Command("TASend")
	harness.WaitFor("the agent's edit", func() bool { return harness.ReadFile("a.txt") == "ONE\n" })

	if path_modifiable(harness, edited) {
		t.Fatal("the leased buffer should not be modifiable while the agent runs")
	}
	if error := harness.Nvim.ExecLua(`vim.api.nvim_buf_call(vim.fn.bufnr(...), function() vim.cmd("normal! ggAx") end)`, nil, edited); error == nil {
		t.Fatal("typing into the leased buffer should fail")
	}
	if !path_modifiable(harness, read) {
		t.Fatal("a buffer the agent only read should stay modifiable")
	}
	type_into(harness, read, "ggAx")
	if got := buffer_lines(harness, read); got != "beex\n" {
		t.Fatalf("read buffer: %q", got)
	}

	held.Gate.Step(t)
	held.Gate.Step(t)
	wait_for_session_end(harness, "foo", "done")
	if !path_modifiable(harness, edited) {
		t.Fatal("the buffer should be modifiable again once the turn ended")
	}
	if got := buffer_lines(harness, edited); got != "ONE\n" {
		t.Fatalf("leased buffer: %q", got)
	}
}

func TestLease_ALeasedBufferWipedAndReopenedMidTurnIsLockedAgain(t *testing.T) {
	held := nvimtest.Reply{ToolCalls: []message.ToolCall{call("tc_2", "read", map[string]any{"path": "a.txt"})}, Gate: nvimtest.NewGate()}
	nvimtest.RegisterProvider(t,
		nvimtest.Reply{ToolCalls: []message.ToolCall{edit_call("tc_1", "a.txt", "one", "ONE")}},
		held,
		nvimtest.Text("done", 10),
	)
	harness := nvimtest.StartWithTools(t, nvimtest.Config(), vimtool_tools)
	edited := filepath.Join(harness.Dir, "a.txt")
	harness.WriteFile("a.txt", "one\n")
	harness.Command("TA foo")
	harness.SetText(harness.Text() + "go\n")
	harness.Command("TASend")
	harness.WaitFor("the agent's edit", func() bool { return harness.ReadFile("a.txt") == "ONE\n" })

	harness.Command("bwipeout! " + edited)
	harness.Command("edit " + edited)
	if path_modifiable(harness, edited) {
		t.Fatal("the reopened leased buffer should not be modifiable while the agent runs")
	}

	held.Gate.Step(t)
	held.Gate.Step(t)
	wait_for_session_end(harness, "foo", "done")
	if !path_modifiable(harness, edited) {
		t.Fatal("the reopened buffer should be modifiable again once the turn ended")
	}
}

func record_modifiable_after_changes(harness *nvimtest.Harness, path string) func() []bool {
	harness.T.Helper()
	code := `
		local buffer = vim.fn.bufnr(...)
		_G.after_changes = {}
		vim.api.nvim_buf_attach(buffer, false, { on_lines = function()
			vim.schedule(function() table.insert(_G.after_changes, vim.bo[buffer].modifiable) end)
		end })`
	if error := harness.Nvim.ExecLua(code, nil, path); error != nil {
		harness.T.Fatal(error)
	}
	return func() []bool {
		var values []bool
		if error := harness.Nvim.ExecLua(`return _G.after_changes`, &values); error != nil {
			harness.T.Fatal(error)
		}
		return values
	}
}

func TestLease_ALeasedBufferIsNeverModifiableBetweenRequestsWhileTheAgentChangesIt(t *testing.T) {
	harness, _ := start_with_vimtool(t,
		edit_call("tc_1", "a.txt", "one", "ONE"),
		edit_call("tc_2", "a.txt", "two", "TWO"),
		call("tc_3", "write", map[string]any{"path": "a.txt", "content": "three\n"}),
		call("tc_4", "filter", map[string]any{"path": "a.txt", "command": "tr a-z A-Z"}))
	path := filepath.Join(harness.Dir, "a.txt")
	harness.WriteFile("a.txt", "one\ntwo\n")
	harness.Command("edit " + path)
	after_changes := record_modifiable_after_changes(harness, path)

	send_agent_turn(harness)

	if got := harness.ReadFile("a.txt"); got != "THREE\n" {
		t.Fatalf("disk: %q", got)
	}
	values := after_changes()
	if len(values) < 4 {
		t.Fatalf("expected a record after each of the 4 changes, got %v", values)
	}
	for _, value := range values {
		if value {
			t.Fatalf("the leased buffer was modifiable after a change: %v", values)
		}
	}
}

func TestLease_BashWriteLeavesOutTheFilesAnotherAgentLeasedAndAppliesTheRest(t *testing.T) {
	held := nvimtest.Reply{ToolCalls: []message.ToolCall{call("one_3", "read", map[string]any{"path": "a.txt"})}, Gate: nvimtest.NewGate()}
	provider := nvimtest.RegisterProvider(t)
	provider.Script("first",
		nvimtest.Reply{ToolCalls: []message.ToolCall{edit_call("one_1", "a.txt", "one", "ONE")}},
		nvimtest.Reply{ToolCalls: []message.ToolCall{edit_call("one_2", "b.txt", "bee", "BEE")}},
		held,
		nvimtest.Text("done", 10))
	provider.Script("second",
		nvimtest.Reply{ToolCalls: []message.ToolCall{call("two_1", "bash_write", map[string]any{"command": "echo x >> a.txt && rm b.txt && echo new > c.txt"})}},
		nvimtest.Text("done", 10))
	config := nvimtest.Config()
	shell := local_bash_write(t, config)
	harness := nvimtest.StartWithTools(t, config, func(client *neovim.Nvim) []tool.Tool {
		return append(vimtool_tools(client), shell(client)...)
	})
	harness.WriteFile("a.txt", "one\n")
	harness.WriteFile("b.txt", "bee\n")

	send_in(harness, "one", "first")
	harness.WaitFor("the first session's edits", func() bool { return harness.ReadFile("b.txt") == "BEE\n" })
	send_in(harness, "two", "second")
	wait_for_session_end(harness, "two", "done")
	held.Gate.Step(t)
	held.Gate.Step(t)
	wait_for_session_end(harness, "one", "done")

	text := result_text(result_of(t, provider, "two_1"))
	for _, line := range []string{
		"a.txt (a.txt is being edited by session one" + LEASE_ADVICE + ")",
		"b.txt (b.txt is being edited by session one" + LEASE_ADVICE + ")",
		"A c.txt",
	} {
		if !strings.Contains(text, line) {
			t.Fatalf("bash_write result should list %q:\n%s", line, text)
		}
	}
	if harness.ReadFile("a.txt") != "ONE\n" || harness.ReadFile("b.txt") != "BEE\n" || harness.ReadFile("c.txt") != "new\n" {
		t.Fatalf("disk: a %q, b %q, c %q", harness.ReadFile("a.txt"), harness.ReadFile("b.txt"), harness.ReadFile("c.txt"))
	}
}

const SESSION_ONE = ".the-agent/sessions/one/session.md"

func TestLease_AnAgentEditingAnotherRunningSessionsFileFailsNamingThatSession(t *testing.T) {
	held := nvimtest.Reply{ToolCalls: []message.ToolCall{call("one_1", "read", map[string]any{"path": "a.txt"})}, Gate: nvimtest.NewGate()}
	provider := nvimtest.RegisterProvider(t)
	provider.Script("first", held, nvimtest.Text("done", 10))
	provider.Script("second",
		nvimtest.Reply{ToolCalls: []message.ToolCall{edit_call("two_1", SESSION_ONE, "first", "FIRST")}},
		nvimtest.Text("done", 10))
	harness := nvimtest.StartWithTools(t, nvimtest.Config(), vimtool_tools)
	harness.WriteFile("a.txt", "one\n")

	send_in(harness, "one", "first")
	held.Gate.Step(t)
	send_in(harness, "two", "second")
	wait_for_session_end(harness, "two", "done")
	held.Gate.Step(t)
	wait_for_session_end(harness, "one", "done")

	refused := result_of(t, provider, "two_1")
	if want := SESSION_ONE + " is being edited by session one" + LEASE_ADVICE; !refused.IsError || result_text(refused) != want {
		t.Fatalf("edit of the running session: got %q (error %v), want %q", result_text(refused), refused.IsError, want)
	}
	if strings.Contains(session_on_disk(harness, "one"), "FIRST") {
		t.Fatalf("the running session's file was changed:\n%s", session_on_disk(harness, "one"))
	}
}

func TestLease_SendingASessionWhoseFileAnAgentLeasedIsRefusedUntilItsTaskEnds(t *testing.T) {
	held := nvimtest.Reply{ToolCalls: []message.ToolCall{call("two_2", "read", map[string]any{"path": SESSION_ONE})}, Gate: nvimtest.NewGate()}
	provider := nvimtest.RegisterProvider(t)
	provider.Script("hello", nvimtest.Text("done", 10))
	provider.Script("second",
		nvimtest.Reply{ToolCalls: []message.ToolCall{edit_call("two_1", SESSION_ONE, "hello", "hello there")}},
		held,
		nvimtest.Text("done", 10))
	harness := nvimtest.StartWithTools(t, nvimtest.Config(), vimtool_tools)
	harness.Command("TA one")
	harness.SetText("## user\n\nhello\n")
	harness.Command("write")

	send_in(harness, "two", "second")
	harness.WaitFor("the agent's edit of one", func() bool { return strings.Contains(session_on_disk(harness, "one"), "hello there") })
	harness.Command("TA one")
	error := harness.CommandError("TASend")
	if error == nil || !strings.Contains(error.Error(), SESSION_ONE+" is being edited by session two") {
		t.Fatalf("sending a leased session: %v", error)
	}
	if !strings.HasSuffix(session_on_disk(harness, "one"), "hello there\n") {
		t.Fatalf("the refused send changed the file:\n%s", session_on_disk(harness, "one"))
	}

	held.Gate.Step(t)
	held.Gate.Step(t)
	wait_for_session_end(harness, "two", "done")
	harness.Command("TA one")
	harness.Command("TASend")
	wait_for_session_end(harness, "one", "done")
}
