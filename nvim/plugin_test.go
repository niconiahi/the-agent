package nvim_test

import (
	"strings"
	"testing"
	"time"

	"github.com/niconiahi/the-agent/nvim/nvimtest"
)

// normal runs keys as typed in normal mode, with <Plug>/<leader> notation.
func normal(harness *nvimtest.Harness, keys string) {
	harness.T.Helper()
	harness.Command(`execute "normal \` + keys + `"`)
}

// mapping is the rhs of a normal-mode mapping ("" when unmapped).
func mapping(harness *nvimtest.Harness, lhs string) string {
	harness.T.Helper()
	var rhs string
	if error := harness.Nvim.Call("maparg", &rhs, lhs, "n"); error != nil {
		harness.T.Fatal(error)
	}
	return rhs
}

func TestPlugTASend_SendsTheSession(t *testing.T) {
	nvimtest.RegisterProvider(t, nvimtest.Text("hi there", 10))
	harness := nvimtest.Start(t, nvimtest.Config())

	harness.Command("TA foo")
	harness.SetText("## user\n\nhello\n")
	normal(harness, "<Plug>(TASend)")

	wait_turn_end(harness, "there")
}

func TestDefaultKeys_SendWithLeaderAS(t *testing.T) {
	nvimtest.RegisterProvider(t, nvimtest.Text("hi there", 10))
	harness := nvimtest.Start(t, nvimtest.Config())

	harness.Command("TA foo")
	harness.SetText("## user\n\nhello\n")
	normal(harness, `\as`) // <leader> is \ by default

	wait_turn_end(harness, "there")
}

func TestPlugTA_PromptsForTheSessionName(t *testing.T) {
	harness := nvimtest.Start(t, nvimtest.Config())

	harness.Command(`lua vim.ui.input = function(_, on_confirm) on_confirm("bar/baz") end`)
	normal(harness, "<Plug>(TA)")

	if !strings.HasSuffix(harness.BufferName(), ".the-agent/sessions/bar-baz/session.md") {
		t.Fatalf("buffer: %s", harness.BufferName())
	}
}

func TestPlugTAAbort_StopsTheTurn(t *testing.T) {
	nvimtest.RegisterProvider(t, slow(1, 300*time.Millisecond, "kept ", "lost"))
	harness := nvimtest.Start(t, nvimtest.Config())

	harness.Command("TA foo")
	harness.SetText("## user\n\nhello\n")
	harness.Command("TASend")
	harness.WaitFor("partial text", func() bool { return strings.Contains(harness.Text(), "kept") })
	normal(harness, "<Plug>(TAAbort)")

	if !modifiable(harness) {
		t.Fatal("the session is still locked after <Plug>(TAAbort)")
	}
	if strings.Contains(harness.Text(), "lost") {
		t.Fatal("the turn kept streaming")
	}
}

func TestDefaultKeys_CanBeOverriddenOrDisabled(t *testing.T) {
	harness := nvimtest.Start(t, nvimtest.Config())

	if got := mapping(harness, "<leader>aa"); got != "<Plug>(TA)" {
		t.Fatalf("<leader>aa: %q", got)
	}
	if got := mapping(harness, "<leader>ax"); got != "<Plug>(TAAbort)" {
		t.Fatalf("<leader>ax: %q", got)
	}

	harness.Setup(`{ keys = { send = "<leader>ss", abort = false } }`)
	if got := mapping(harness, "<leader>ss"); got != "<Plug>(TASend)" {
		t.Fatalf("overridden send key: %q", got)
	}
	for _, lhs := range []string{"<leader>as", "<leader>ax"} {
		if got := mapping(harness, lhs); got != "" {
			t.Fatalf("%s still mapped to %q", lhs, got)
		}
	}
	if got := mapping(harness, "<leader>aa"); got != "<Plug>(TA)" {
		t.Fatalf("untouched default lost: %q", got)
	}

	harness.Setup(`{ keys = false }`)
	for _, lhs := range []string{"<leader>aa", "<leader>ss"} {
		if got := mapping(harness, lhs); got != "" {
			t.Fatalf("%s still mapped to %q", lhs, got)
		}
	}
}

func TestTA_UsesTheConfiguredSessionName(t *testing.T) {
	harness := nvimtest.Start(t, nvimtest.Config())
	harness.Setup(`{ name = function(input) return "2026-10-06/" .. input end }`)

	harness.Command("TA foo")

	if !strings.HasSuffix(harness.BufferName(), ".the-agent/sessions/2026-10-06-foo/session.md") {
		t.Fatalf("buffer: %s", harness.BufferName())
	}
}

func TestTA_ConfiguredNameCanMakeTheArgumentOptional(t *testing.T) {
	harness := nvimtest.Start(t, nvimtest.Config())
	if error := harness.CommandError("TA"); error == nil || !strings.Contains(error.Error(), "usage") {
		t.Fatalf("bare :TA without a naming function: %v", error)
	}

	harness.Setup(`{ name = function(input) return input == "" and "scratch" or input end }`)
	harness.Command("TA")

	if !strings.HasSuffix(harness.BufferName(), ".the-agent/sessions/scratch/session.md") {
		t.Fatalf("buffer: %s", harness.BufferName())
	}
}

// failed_turn sends a turn the fake provider fails, and returns the
// notifications recorded by the Lua function installed as `recorder` (a
// Lua assignment target such as "vim.notify").
func failed_turn(t *testing.T, recorder string) []string {
	nvimtest.RegisterProvider(t) // no scripted reply: the turn errors
	harness := nvimtest.Start(t, nvimtest.Config())
	harness.Command(`lua _G.notes = {}`)
	harness.Command(`lua ` + recorder + ` = function(msg, a, b)
		local opts = type(a) == "table" and a or b or {}
		table.insert(_G.notes, (opts.title or "") .. ": " .. msg)
	end`)

	harness.Command("TA foo")
	harness.SetText("## user\n\nhello\n")
	harness.Command("TASend")

	var notes []string
	harness.WaitFor("a notification", func() bool {
		if error := harness.Nvim.ExecLua(`return _G.notes`, &notes); error != nil {
			t.Fatal(error)
		}
		return len(notes) > 0
	})
	return notes
}

func TestNotify_UsesSnacksWhenPresent(t *testing.T) {
	notes := failed_turn(t, `package.loaded.snacks = { notify = nil }; package.loaded.snacks.notify`)
	if !strings.HasPrefix(notes[0], "the-agent: ") || !strings.Contains(notes[0], "no scripted reply left") {
		t.Fatalf("snacks got %q", notes)
	}
}

func TestNotify_FallsBackToVimNotify(t *testing.T) {
	notes := failed_turn(t, `vim.notify`)
	if !strings.HasPrefix(notes[0], "the-agent: ") || !strings.Contains(notes[0], "no scripted reply left") {
		t.Fatalf("vim.notify got %q", notes)
	}
}

func TestDefaultKeys_RegisterTheAiGroupWithWhichKey(t *testing.T) {
	harness := nvimtest.Launch(t)
	harness.Command(`lua package.loaded["which-key"] = { add = function(spec) _G.which_key_spec = spec end }`)
	harness.Setup(`{ chan = ... }`, harness.Nvim.ChannelID())

	var group string
	if error := harness.Nvim.ExecLua(`local s = _G.which_key_spec[1]; return s[1] .. "=" .. s.group`, &group); error != nil {
		t.Fatal(error)
	}
	if group != "<leader>a=ai" {
		t.Fatalf("which-key group: %q", group)
	}
}
