package nvim_test

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

// render draws the current window's statusline the way Neovim would.
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

	harness.Command("TA foo")

	// A new session costs its system prompt: "You are a test agent." is 21
	// bytes, 6 tokens.
	harness.WaitFor("the count", func() bool { return strings.Contains(render(harness).Text, "6 / 200,000 tokens") })
	if render(harness).red() {
		t.Fatal("far from the ceiling the count must not be red")
	}
}

func TestStatusline_FollowsEdits(t *testing.T) {
	harness := nvimtest.Start(t, nvimtest.Config())

	harness.Command("TA foo")
	harness.SetText(harness.Text() + strings.Repeat("x", 400) + "\n")
	harness.Command("doautocmd TextChanged")

	// What :TASend would send: the 21-byte system prompt, then the 400 bytes
	// under a timestamp ("2026-10-06T14:32:00Z\n\n", 22 bytes) = 443 bytes =
	// 111 tokens.
	harness.WaitFor("the new count", func() bool { return strings.Contains(render(harness).Text, "111 / 200,000 tokens") })
}

func TestStatusline_TurnsRedNearCeiling(t *testing.T) {
	harness := nvimtest.Start(t, nvimtest.Config())
	harness.Setup(`{ ceiling = 6 }`)

	harness.Command("TA foo")

	harness.WaitFor("a red count", func() bool {
		drawn := render(harness)
		return strings.Contains(drawn.Text, "6 / 6 tokens") && drawn.red()
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
		return len(count) >= 4 // above 1,000 tokens
	})
}

func TestStatusline_LeavesOtherBuffersAlone(t *testing.T) {
	harness := nvimtest.Start(t, nvimtest.Config())

	harness.Command("TA foo")
	harness.Command("enew")

	if text := render(harness).Text; strings.Contains(text, "tokens") {
		t.Fatalf("a non-session buffer must not show a count: %q", text)
	}
}
