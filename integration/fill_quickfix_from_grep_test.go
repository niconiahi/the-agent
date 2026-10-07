package integration_test

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/niconiahi/the-agent/message"
	"github.com/niconiahi/the-agent/nvim"
	"github.com/niconiahi/the-agent/nvim/nvimtest"
	"github.com/niconiahi/the-agent/vimtool"
)

type quickfix_entry struct {
	File string `msgpack:"file"`
	Line int    `msgpack:"line"`
	Text string `msgpack:"text"`
}

func grep_turn(t *testing.T, harness *nvimtest.Harness, arguments map[string]any) string {
	t.Helper()
	provider := nvimtest.RegisterProvider(t,
		nvimtest.Reply{ToolCalls: []message.ToolCall{{ID: "tc_1", Name: "grep", Arguments: arguments}}},
		nvimtest.Text("done", 10),
	)
	config := nvimtest.Config()
	config.Project = harness.Dir
	config.Tools = append(config.Tools, vimtool.Grep(harness.Nvim))
	if error := nvim.Attach(harness.Nvim, config); error != nil {
		t.Fatal(error)
	}
	harness.Setup(`{ chan = ... }`, harness.Nvim.ChannelID())

	harness.Command("TA foo")
	harness.SetText(harness.Text() + "find two\n")
	harness.Command("TASend")
	harness.WaitFor("the turn", func() bool { return strings.Contains(harness.ReadFile(SESSION), "\ndone\n") })

	result := provider.Requests()[1].Messages[2].(message.ToolResultMessage)
	if result.IsError {
		t.Fatalf("grep failed: %#v", result)
	}
	return result.Content[0].(message.TextContent).Text
}

func quickfix(t *testing.T, harness *nvimtest.Harness) []quickfix_entry {
	t.Helper()
	var entries []quickfix_entry
	error := harness.Nvim.ExecLua(`
		local entries = {}
		for _, item in ipairs(vim.fn.getqflist()) do
			table.insert(entries, { file = vim.fn.fnamemodify(vim.fn.bufname(item.bufnr), ":p"), line = item.lnum, text = item.text })
		end
		return entries
	`, &entries)
	if error != nil {
		t.Fatal(error)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].File != entries[j].File {
			return entries[i].File < entries[j].File
		}
		return entries[i].Line < entries[j].Line
	})
	return entries
}

func sorted_lines(text string) []string {
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	sort.Strings(lines)
	return lines
}

func TestGrep_FillsQuickfixWithEveryHitAndKeepsResult(t *testing.T) {
	harness := nvimtest.Launch(t)
	harness.WriteFile("a.txt", "one\ntwo\nthree\n")
	harness.WriteFile("sub/b.txt", "two words: here\nnothing\n")
	a := filepath.Join(harness.Dir, "a.txt")
	b := filepath.Join(harness.Dir, "sub", "b.txt")

	result := grep_turn(t, harness, map[string]any{"pattern": "two", "path": harness.Dir})

	want_result := []string{a + ":2:two", b + ":1:two words: here"}
	if got := sorted_lines(result); strings.Join(got, "\n") != strings.Join(want_result, "\n") || !strings.HasSuffix(result, "\n") {
		t.Fatalf("tool result: %q", result)
	}
	want := []quickfix_entry{{a, 2, "two"}, {b, 1, "two words: here"}}
	got := quickfix(t, harness)
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("quickfix: %#v", got)
	}
}

func TestGrep_FillsQuickfixForASingleFileSearch(t *testing.T) {
	harness := nvimtest.Launch(t)
	harness.WriteFile("a.txt", "one\ntwo\nthree two\n")
	a := filepath.Join(harness.Dir, "a.txt")

	result := grep_turn(t, harness, map[string]any{"pattern": "two", "path": a})

	if result != "2:two\n3:three two\n" {
		t.Fatalf("tool result: %q", result)
	}
	want := []quickfix_entry{{a, 2, "two"}, {a, 3, "three two"}}
	got := quickfix(t, harness)
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("quickfix: %#v", got)
	}
}
