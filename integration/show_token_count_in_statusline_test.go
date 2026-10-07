package integration_test

import (
	"strings"
	"testing"

	"github.com/niconiahi/the-agent/nvim/nvimtest"
)

type statusline struct {
	Text       string `msgpack:"str"`
	Highlights []struct {
		Start int    `msgpack:"start"`
		Group string `msgpack:"group"`
	} `msgpack:"highlights"`
}

func render(harness *nvimtest.Harness) statusline {
	harness.T.Helper()
	var drawn statusline
	error := harness.Nvim.ExecLua(`
		local win = vim.api.nvim_get_current_win()
		return vim.api.nvim_eval_statusline(vim.wo[win].statusline, { winid = win, highlights = true })
	`, &drawn)
	if error != nil {
		harness.T.Fatal(error)
	}
	return drawn
}

func (drawn statusline) red() bool {
	for _, highlight := range drawn.Highlights {
		if highlight.Group == "TheAgentTokensNear" {
			return true
		}
	}
	return false
}

func TestStatusline_ShowsCountAgainstCeiling(t *testing.T) {
	harness := nvimtest.Start(t, nvimtest.Config())
	harness.WriteFile(".the-agent/system_prompt.md", nvimtest.Config().SystemPrompt+"\n")

	harness.Command("TA foo")

	harness.WaitFor("the count", func() bool { return strings.Contains(render(harness).Text, "14 / 200,000 tokens") })
	if render(harness).red() {
		t.Fatal("far from the ceiling the count must not be red")
	}
}

func TestStatusline_FollowsEdits(t *testing.T) {
	harness := nvimtest.Start(t, nvimtest.Config())
	harness.WriteFile(".the-agent/system_prompt.md", nvimtest.Config().SystemPrompt+"\n")

	harness.Command("TA foo")
	harness.SetText(harness.Text() + strings.Repeat("x", 400) + "\n")
	harness.Command("doautocmd TextChanged")

	harness.WaitFor("the new count", func() bool { return strings.Contains(render(harness).Text, "119 / 200,000 tokens") })
}

func TestStatusline_TurnsRedNearCeiling(t *testing.T) {
	harness := nvimtest.Start(t, nvimtest.Config())
	harness.WriteFile(".the-agent/system_prompt.md", nvimtest.Config().SystemPrompt+"\n")
	harness.Setup(`{ ceiling = 14 }`)

	harness.Command("TA foo")

	harness.WaitFor("a red count", func() bool {
		drawn := render(harness)
		return strings.Contains(drawn.Text, "14 / 14 tokens") && drawn.red()
	})
}

func TestStatusline_CeilingIsClampedToContextWindow(t *testing.T) {
	harness := nvimtest.Start(t, nvimtest.Config())
	harness.Setup(`{ ceiling = 1000000 }`)

	harness.Command("TA foo")

	harness.WaitFor("the clamped ceiling", func() bool { return strings.Contains(render(harness).Text, "/ 262,144 tokens") })
}

func TestStatusline_UpdatesAfterReply(t *testing.T) {
	nvimtest.RegisterProvider(t, nvimtest.Text(strings.Repeat("y", 4000), 1))
	harness := nvimtest.Start(t, nvimtest.Config())

	harness.Command("TA foo")
	harness.SetText(harness.Text() + "hi\n")
	harness.Command("TASend")
	harness.WaitFor("reply", func() bool { return strings.Contains(harness.Text(), "yyyy") })

	harness.WaitFor("a count that includes the reply", func() bool {
		text := render(harness).Text
		index := strings.Index(text, " / 200,000 tokens")
		if index < 0 {
			return false
		}
		fields := strings.Fields(text[:index])
		count := strings.ReplaceAll(fields[len(fields)-1], ",", "")
		return len(count) >= 4
	})
}

func TestStatusline_KeepsTheUsersOwnStatusline(t *testing.T) {
	harness := nvimtest.Start(t, nvimtest.Config())
	harness.Command("set statusline=mine")

	harness.Command("TA foo")

	var local string
	if error := harness.Nvim.ExecLua(`return vim.wo.statusline`, &local); error != nil {
		t.Fatal(error)
	}
	if local != "mine" {
		t.Fatalf("the user's statusline was replaced by %q", local)
	}
}

func TestStatusline_LeavesOtherBuffersAlone(t *testing.T) {
	harness := nvimtest.Start(t, nvimtest.Config())

	harness.Command("TA foo")
	harness.Command("enew")

	if text := render(harness).Text; strings.Contains(text, "tokens") {
		t.Fatalf("a non-session buffer must not show a count: %q", text)
	}
}
