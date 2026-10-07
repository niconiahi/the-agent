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

// The edit's arguments, split mid-key, mid-escape and mid-UTF-8 the way a
// model streams them.
var EDIT_FRAGMENTS = []string{
	`{"pa`,
	`th":"a.t`,
	`xt","old_te`,
	`xt":"two\`,
	`nthree","new_`,
	`text":"TWO\nTHR`,
	"EE caf\xc3",
	"\xa9 \\u00",
	`e9\nFOUR"}`,
}

func streamed_edit() message.ToolCall {
	return call("tc_1", "edit", map[string]any{"path": "a.txt", "old_text": "two\nthree", "new_text": "TWO\nTHREE café é\nFOUR"})
}

type gated_turn struct {
	harness *nvimtest.Harness
	gate    *nvimtest.Gate
	window  int
	buffer  int
	path    string
}

// start_gated_edit opens a.txt's session, sends a turn whose reply streams
// the edit through a gate, and returns before the first fragment.
func start_gated_edit(t *testing.T, fragments []string, edit message.ToolCall, after ...nvimtest.Reply) *gated_turn {
	t.Helper()
	gated := nvimtest.Reply{ToolCalls: []message.ToolCall{edit}, Fragments: [][]string{fragments}, Gate: nvimtest.NewGate()}
	return start_gated_turn(t, append([]nvimtest.Reply{gated}, after...)...)
}

// start_gated_turn is start_gated_edit for scripted replies, the first of
// which holds the gate.
func start_gated_turn(t *testing.T, replies ...nvimtest.Reply) *gated_turn {
	t.Helper()
	nvimtest.RegisterProvider(t, replies...)
	harness := nvimtest.StartWithTools(t, nvimtest.Config(), vimtool_tools)
	harness.WriteFile("a.txt", "one\ntwo\nthree\nfive\n")
	harness.Command("TA foo")
	harness.SetText(harness.Text() + "go\n")
	turn := &gated_turn{harness: harness, gate: replies[0].Gate, path: filepath.Join(harness.Dir, "a.txt")}
	turn.window = current_window(harness)
	turn.buffer = current_buffer(harness)
	harness.Command("TASend")
	return turn
}

// step lets one more fragment through and checks I am still where I was.
func (turn *gated_turn) step() {
	turn.harness.T.Helper()
	turn.gate.Step(turn.harness.T)
	turn.still_in_my_window()
}

func (turn *gated_turn) still_in_my_window() {
	turn.harness.T.Helper()
	if window, buffer := current_window(turn.harness), current_buffer(turn.harness); window != turn.window || buffer != turn.buffer {
		turn.harness.T.Fatalf("current window moved: window %d buffer %d, was window %d buffer %d", window, buffer, turn.window, turn.buffer)
	}
}

func current_window(harness *nvimtest.Harness) int {
	harness.T.Helper()
	var window int
	if error := harness.Nvim.ExecLua(`return vim.api.nvim_get_current_win()`, &window); error != nil {
		harness.T.Fatal(error)
	}
	return window
}

func current_buffer(harness *nvimtest.Harness) int {
	harness.T.Helper()
	var buffer int
	if error := harness.Nvim.ExecLua(`return vim.api.nvim_get_current_buf()`, &buffer); error != nil {
		harness.T.Fatal(error)
	}
	return buffer
}

// follow_window_file is the file the follow window shows, "" when there is
// no follow window.
func follow_window_file(harness *nvimtest.Harness) string {
	harness.T.Helper()
	var name string
	code := `for _, win in ipairs(vim.api.nvim_list_wins()) do
		if vim.w[win].the_agent_follow then return vim.api.nvim_buf_get_name(vim.api.nvim_win_get_buf(win)) end
	end
	return ""`
	if error := harness.Nvim.ExecLua(code, &name); error != nil {
		harness.T.Fatal(error)
	}
	return name
}

// preview is what the preview extmarks on a file's buffer show.
type preview struct {
	Marks   int      `msgpack:"marks"`
	Region  []int    `msgpack:"region"`
	Group   string   `msgpack:"group"`
	Virtual []string `msgpack:"virtual"`
}

func preview_of(harness *nvimtest.Harness, path string) preview {
	harness.T.Helper()
	var shown preview
	code := `local buffer = vim.fn.bufnr(...)
	local namespace = vim.api.nvim_get_namespaces()["the-agent-preview"]
	local shown = { marks = 0, region = {}, group = "", virtual = {} }
	if buffer < 0 or not namespace then return shown end
	for _, mark in ipairs(vim.api.nvim_buf_get_extmarks(buffer, namespace, 0, -1, { details = true })) do
		shown.marks = shown.marks + 1
		local details = mark[4]
		if details.hl_group then
			shown.region = { mark[2], mark[3], details.end_row, details.end_col }
			shown.group = details.hl_group
		end
		for _, line in ipairs(details.virt_lines or {}) do
			local text = ""
			for _, chunk in ipairs(line) do text = text .. chunk[1] end
			table.insert(shown.virtual, text)
		end
	end
	return shown`
	if error := harness.Nvim.ExecLua(code, &shown, path); error != nil {
		harness.T.Fatal(error)
	}
	return shown
}

func follow_window_cursor_line(harness *nvimtest.Harness) int {
	harness.T.Helper()
	var line int
	code := `for _, win in ipairs(vim.api.nvim_list_wins()) do
		if vim.w[win].the_agent_follow then return vim.api.nvim_win_get_cursor(win)[1] end
	end
	return 0`
	if error := harness.Nvim.ExecLua(code, &line); error != nil {
		harness.T.Fatal(error)
	}
	return line
}

func TestEditPreview_PlaysOutInTheFollowWindowAsTheArgumentsStream(t *testing.T) {
	turn := start_gated_edit(t, EDIT_FRAGMENTS, streamed_edit(), nvimtest.Text("done", 10))
	harness := turn.harness

	turn.step() // {"pa
	turn.step() // th":"a.t
	if got := follow_window_file(harness); got != "" {
		t.Fatalf("follow window before path completed: %q", got)
	}

	turn.step() // xt","old_te
	harness.WaitFor("the follow window to show a.txt", func() bool { return follow_window_file(harness) == turn.path })
	turn.still_in_my_window()

	turn.step() // xt":"two\
	if got := preview_of(harness, turn.path); got.Marks != 0 {
		t.Fatalf("preview before old_text completed: %#v", got)
	}

	turn.step() // nthree","new_
	harness.WaitFor("the region to be highlighted", func() bool { return len(preview_of(harness, turn.path).Region) == 4 })
	if got := preview_of(harness, turn.path); !slices.Equal(got.Region, []int{1, 0, 2, 5}) || got.Group != "TheAgentPreviewOld" {
		t.Fatalf("highlight: %#v", got)
	}
	if got := follow_window_cursor_line(harness); got != 2 {
		t.Fatalf("follow window cursor on line %d, want the region's first line 2", got)
	}
	tick := buffer_tick(harness, turn.path)

	for _, stage := range []struct {
		fragment string
		virtual  []string
	}{
		{`text":"TWO\nTHR`, []string{"TWO", "THR"}},
		{"EE caf\\xc3", []string{"TWO", "THREE caf"}},
		{`\xa9 \\u00`, []string{"TWO", "THREE café "}},
		{`e9\nFOUR"}`, []string{"TWO", "THREE café é", "FOUR"}},
	} {
		turn.step()
		harness.WaitFor("virtual text after "+stage.fragment, func() bool {
			return slices.Equal(preview_of(harness, turn.path).Virtual, stage.virtual)
		})
		if got := buffer_lines(harness, turn.path); got != "one\ntwo\nthree\nfive\n" {
			t.Fatalf("buffer text changed by the preview after %s: %q", stage.fragment, got)
		}
		if got := buffer_tick(harness, turn.path); got != tick {
			t.Fatalf("changedtick moved from %d to %d after %s", tick, got, stage.fragment)
		}
	}

	turn.step() // ToolCallEnd
	turn.wait_for_turn_end()
	turn.still_in_my_window()
	const edited = "one\nTWO\nTHREE café é\nFOUR\nfive\n"
	if got := buffer_lines(harness, turn.path); got != edited {
		t.Fatalf("buffer after the edit: %q", got)
	}
	if got := harness.ReadFile("a.txt"); got != edited {
		t.Fatalf("disk after the edit: %q", got)
	}
	if got := preview_of(harness, turn.path); got.Marks != 0 {
		t.Fatalf("preview extmarks left after the edit: %#v", got)
	}
	if got := follow_window_file(harness); got != turn.path {
		t.Fatalf("follow window after the edit: %q", got)
	}
	undo(harness, turn.path)
	if got := buffer_lines(harness, turn.path); got != "one\ntwo\nthree\nfive\n" {
		t.Fatalf("after one undo: %q", got)
	}
}

func (turn *gated_turn) wait_for_turn_end() {
	turn.harness.T.Helper()
	path := nvim.SessionPath(turn.harness.Dir, "foo")
	turn.harness.WaitFor("the turn to end", func() bool {
		contents, _ := os.ReadFile(path)
		return strings.HasSuffix(string(contents), "\ndone\n\n## user\n\n")
	})
}

// stream_into_new_text steps the edit until its new_text is drawn halfway.
func (turn *gated_turn) stream_into_new_text() {
	turn.harness.T.Helper()
	for range 6 {
		turn.step()
	}
	turn.harness.WaitFor("new_text to be drawn", func() bool {
		return slices.Equal(preview_of(turn.harness, turn.path).Virtual, []string{"TWO", "THR"})
	})
}

// untouched checks a.txt's buffer and file are exactly what they were and no
// preview is left on it.
func (turn *gated_turn) untouched(tick int) {
	turn.harness.T.Helper()
	turn.harness.WaitFor("the preview to be cleared", func() bool { return preview_of(turn.harness, turn.path).Marks == 0 })
	turn.still_in_my_window()
	if got := buffer_lines(turn.harness, turn.path); got != "one\ntwo\nthree\nfive\n" {
		turn.harness.T.Fatalf("buffer: %q", got)
	}
	if got := buffer_tick(turn.harness, turn.path); got != tick {
		turn.harness.T.Fatalf("changedtick moved from %d to %d", tick, got)
	}
	if got := turn.harness.ReadFile("a.txt"); got != "one\ntwo\nthree\nfive\n" {
		turn.harness.T.Fatalf("disk: %q", got)
	}
}

func TestEditPreview_AbortClearsThePreviewAndLeavesTheFileUntouched(t *testing.T) {
	turn := start_gated_edit(t, EDIT_FRAGMENTS, streamed_edit())
	turn.stream_into_new_text()
	tick := buffer_tick(turn.harness, turn.path)

	turn.harness.Command("TAAbort")

	turn.untouched(tick)
}

func TestEditPreview_AStreamThatFailsMidCallClearsThePreviewAndLeavesTheFileUntouched(t *testing.T) {
	failing := nvimtest.Reply{
		ToolCalls:    []message.ToolCall{streamed_edit()},
		Fragments:    [][]string{EDIT_FRAGMENTS[:6]},
		Gate:         nvimtest.NewGate(),
		StopReason:   message.STOP_REASON_ERROR,
		ErrorMessage: "connection reset",
	}
	turn := start_gated_turn(t, failing)
	turn.stream_into_new_text()
	tick := buffer_tick(turn.harness, turn.path)

	turn.step() // ToolCallEnd, then the error

	turn.untouched(tick)
}

func TestEditPreview_AnEditThatFailsClearsThePreviewAndLeavesTheFileUntouched(t *testing.T) {
	fragments := []string{`{"path":"a.txt","old_text":"two\nthree"`, `,"new":"TWO"}`}
	turn := start_gated_edit(t, fragments, call("tc_1", "edit", map[string]any{"path": "a.txt", "old_text": "two\nthree", "new": "TWO"}), nvimtest.Text("done", 10))
	turn.step()
	turn.harness.WaitFor("the region to be highlighted", func() bool { return len(preview_of(turn.harness, turn.path).Region) == 4 })
	tick := buffer_tick(turn.harness, turn.path)

	turn.step()
	turn.step() // ToolCallEnd: the edit runs and fails

	turn.wait_for_turn_end()
	turn.untouched(tick)
}
