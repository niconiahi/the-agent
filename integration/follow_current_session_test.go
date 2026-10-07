package integration_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/niconiahi/the-agent/message"
	"github.com/niconiahi/the-agent/nvim"
	"github.com/niconiahi/the-agent/nvim/nvimtest"
)

func explorers_that_edit() nvim.Config {
	config := nvimtest.Config()
	config.ExplorerTools = []string{"read", "grep", "find", "ls", "bash_read", "edit"}
	return config
}

func gated_edit(id string, path string, old_text string, new_text string) nvimtest.Reply {
	return nvimtest.Reply{
		ToolCalls: []message.ToolCall{call(id, "edit", map[string]any{"path": path, "old_text": old_text, "new_text": new_text})},
		Fragments: [][]string{{
			`{"path":"` + path + `",`,
			`"old_text":"` + old_text + `","new_text":"` + new_text + `"}`,
		}},
		Gate: nvimtest.NewGate(),
	}
}

func open_follow_window(harness *nvimtest.Harness, relative string) {
	harness.T.Helper()
	code := `local buffer = vim.fn.bufadd(...)
	vim.fn.bufload(buffer)
	local win = vim.api.nvim_open_win(buffer, false, { split = "right", win = -1 })
	vim.w[win].the_agent_follow = true`
	if error := harness.Nvim.ExecLua(code, nil, filepath.Join(harness.Dir, relative)); error != nil {
		harness.T.Fatal(error)
	}
}

func wait_for_session_end(harness *nvimtest.Harness, session string, last string) {
	harness.T.Helper()
	harness.WaitFor(session+" to end", func() bool {
		return strings.HasSuffix(session_on_disk(harness, session), "\n"+last+"\n\n## user\n\n")
	})
}

func TestFollowWindow_FollowsAnEditByASubagentOfTheSessionIWasLastIn(t *testing.T) {
	edit := gated_edit("c1", "a.txt", "two", "TWO")
	provider := nvimtest.RegisterProvider(t,
		nvimtest.Reply{ToolCalls: []message.ToolCall{call("t1", "task", map[string]any{"job": JOB})}},
		nvimtest.Text("done", 10),
	)
	provider.Script(JOB, edit, nvimtest.Text("edited a.txt", 7))
	harness := nvimtest.StartWithTools(t, explorers_that_edit(), vimtool_tools)
	harness.WriteFile("a.txt", "one\ntwo\nthree\n")
	harness.Command("TA foo")
	harness.SetText(harness.Text() + "go\n")
	turn := &gated_turn{harness: harness, gate: edit.Gate, path: filepath.Join(harness.Dir, "a.txt")}
	turn.window, turn.buffer = current_window(harness), current_buffer(harness)
	harness.Command("TASend")

	turn.step()
	harness.WaitFor("the follow window to show the subagent's file", func() bool { return follow_window_file(harness) == turn.path })
	turn.step()
	harness.WaitFor("the subagent's region to be highlighted", func() bool {
		shown := preview_of(harness, turn.path)
		return slices.Equal(shown.Region, []int{1, 0, 1, 3}) && slices.Equal(shown.Virtual, []string{"TWO"})
	})
	turn.still_in_my_window()

	turn.step()
	wait_for_session_end(harness, "foo", "done")
	if got := harness.ReadFile("a.txt"); got != "one\nTWO\nthree\n" {
		t.Fatalf("a.txt after the subagent's edit: %q", got)
	}
	harness.WaitFor("the subagent's preview to be cleared", func() bool { return preview_of(harness, turn.path).Marks == 0 })
	turn.still_in_my_window()
}

func TestFollowWindow_AnEditByAnotherRunningSessionOnlyNotifies(t *testing.T) {
	edit := gated_edit("e1", "a.txt", "two", "TWO")
	provider := nvimtest.RegisterProvider(t)
	provider.Script("alpha", edit, nvimtest.Text("alpha done", 5))
	harness := nvimtest.StartWithTools(t, nvimtest.Config(), vimtool_tools)
	harness.WriteFile("a.txt", "one\ntwo\nthree\n")
	harness.WriteFile("notes.txt", "mine\n")
	notifications := record_notifications(harness)

	send_in(harness, "other", "alpha")
	harness.Command("TA mine")
	open_follow_window(harness, "notes.txt")
	notes_path := filepath.Join(harness.Dir, "notes.txt")
	a_path := filepath.Join(harness.Dir, "a.txt")
	window, buffer := current_window(harness), current_buffer(harness)

	edit.Gate.Step(t)
	harness.WaitFor("a notification", func() bool { return len(notifications()) > 0 })
	if got := notifications(); !slices.Equal(got, []string{"other is editing a.txt"}) {
		t.Fatalf("notifications: %q", got)
	}
	edit.Gate.Step(t)
	edit.Gate.Step(t)
	wait_for_session_end(harness, "other", "alpha done")

	if got := follow_window_file(harness); got != notes_path {
		t.Fatalf("the follow window moved to %q", got)
	}
	if got := preview_of(harness, a_path); got.Marks != 0 {
		t.Fatalf("preview of another session's edit: %#v", got)
	}
	if got := harness.ReadFile("a.txt"); got != "one\nTWO\nthree\n" {
		t.Fatalf("the other session's edit should still land: %q", got)
	}
	if current_window(harness) != window || current_buffer(harness) != buffer {
		t.Fatal("the current window moved")
	}
	if got := notifications(); len(got) != 1 {
		t.Fatalf("one edit should notify once: %q", got)
	}
}

func TestFollowWindow_SwitchingToAnotherSessionsFileRetargetsIt(t *testing.T) {
	alpha := gated_edit("e1", "a.txt", "two", "TWO")
	beta := gated_edit("e2", "b.txt", "bee", "BEE")
	provider := nvimtest.RegisterProvider(t)
	provider.Script("alpha", alpha, nvimtest.Text("alpha done", 5))
	provider.Script("beta", beta, nvimtest.Text("beta done", 5))
	harness := nvimtest.StartWithTools(t, nvimtest.Config(), vimtool_tools)
	harness.WriteFile("a.txt", "one\ntwo\nthree\n")
	harness.WriteFile("b.txt", "bee\n")
	notifications := record_notifications(harness)
	a_path, b_path := filepath.Join(harness.Dir, "a.txt"), filepath.Join(harness.Dir, "b.txt")

	send_in(harness, "one", "alpha")
	send_in(harness, "two", "beta")

	beta.Gate.Step(t)
	harness.WaitFor("the follow window to show two's file", func() bool { return follow_window_file(harness) == b_path })
	alpha.Gate.Step(t)
	harness.WaitFor("a notification about one", func() bool { return slices.Contains(notifications(), "one is editing a.txt") })
	if got := follow_window_file(harness); got != b_path {
		t.Fatalf("one's edit moved the follow window to %q", got)
	}

	var escaped string
	if error := harness.Nvim.Call("fnameescape", &escaped, nvim.SessionPath(harness.Dir, "one")); error != nil {
		t.Fatal(error)
	}
	harness.Command("edit " + escaped)
	window, buffer := current_window(harness), current_buffer(harness)

	alpha.Gate.Step(t)
	harness.WaitFor("the follow window to show one's file", func() bool { return follow_window_file(harness) == a_path })
	harness.WaitFor("one's region to be highlighted", func() bool {
		return slices.Equal(preview_of(harness, a_path).Region, []int{1, 0, 1, 3})
	})
	beta.Gate.Step(t)
	beta.Gate.Step(t)
	wait_for_session_end(harness, "two", "beta done")
	if got := follow_window_file(harness); got != a_path {
		t.Fatalf("two's edit moved the follow window to %q after I switched to one", got)
	}
	if got := notifications(); !slices.Contains(got, "two is editing b.txt") {
		t.Fatalf("two's edit after the switch should notify: %q", got)
	}
	alpha.Gate.Step(t)
	wait_for_session_end(harness, "one", "alpha done")
	if current_window(harness) != window || current_buffer(harness) != buffer {
		t.Fatal("the current window moved")
	}
}

func enter_file(harness *nvimtest.Harness, command string, path string) {
	harness.T.Helper()
	var escaped string
	if error := harness.Nvim.Call("fnameescape", &escaped, path); error != nil {
		harness.T.Fatal(error)
	}
	harness.Command(command + " " + escaped)
}

func TestFollowWindow_EnteringASubagentsFileKeepsFollowingItsRootSession(t *testing.T) {
	edit := gated_edit("e1", "a.txt", "two", "TWO")
	provider := nvimtest.RegisterProvider(t,
		nvimtest.Reply{ToolCalls: []message.ToolCall{call("t1", "task", map[string]any{"job": JOB})}},
		edit,
		nvimtest.Text("done", 10),
	)
	provider.Script(JOB, nvimtest.Text("found", 7))
	harness := nvimtest.StartWithTools(t, nvimtest.Config(), vimtool_tools)
	harness.WriteFile("a.txt", "one\ntwo\nthree\n")
	notifications := record_notifications(harness)
	a_path := filepath.Join(harness.Dir, "a.txt")

	send_in(harness, "foo", "go")
	child := child_session(harness, "01-map-callers-of-foo")
	harness.WaitFor("the child to end", func() bool {
		contents, _ := os.ReadFile(child)
		return strings.HasSuffix(string(contents), "\nfound\n\n## user\n\n")
	})
	enter_file(harness, "edit", child)

	edit.Gate.Step(t)
	harness.WaitFor("the follow window to show the parent's file", func() bool { return follow_window_file(harness) == a_path })
	edit.Gate.Step(t)
	harness.WaitFor("the parent's region to be highlighted", func() bool {
		return slices.Equal(preview_of(harness, a_path).Region, []int{1, 0, 1, 3})
	})
	edit.Gate.Step(t)
	wait_for_session_end(harness, "foo", "done")
	if got := notifications(); len(got) != 0 {
		t.Fatalf("the parent of the session I am in should be followed, not notified: %q", got)
	}
}

func TestFollowWindow_SendingFromASessionFollowsIt(t *testing.T) {
	edit := gated_edit("e1", "a.txt", "two", "TWO")
	provider := nvimtest.RegisterProvider(t)
	provider.Script("alpha", edit, nvimtest.Text("alpha done", 5))
	harness := nvimtest.StartWithTools(t, nvimtest.Config(), vimtool_tools)
	harness.WriteFile("a.txt", "one\ntwo\nthree\n")
	notifications := record_notifications(harness)
	a_path := filepath.Join(harness.Dir, "a.txt")
	harness.Command("TA two")
	harness.Command("TA one")

	enter_file(harness, "noautocmd edit", nvim.SessionPath(harness.Dir, "two"))
	harness.SetText("## user\n\nalpha\n")
	harness.Command("TASend")

	edit.Gate.Step(t)
	harness.WaitFor("the follow window to show the sent session's file", func() bool { return follow_window_file(harness) == a_path })
	edit.Gate.Step(t)
	edit.Gate.Step(t)
	wait_for_session_end(harness, "two", "alpha done")
	if got := notifications(); len(got) != 0 {
		t.Fatalf("the session I sent from should be followed, not notified: %q", got)
	}
}
