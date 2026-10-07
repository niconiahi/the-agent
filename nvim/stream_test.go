package nvim_test

import (
	"strings"
	"testing"
	"time"

	"github.com/niconiahi/the-agent/nvim/nvimtest"
)

func slow(total_tokens int, delay time.Duration, deltas ...string) nvimtest.Reply {
	return nvimtest.Reply{Deltas: deltas, Delay: delay, TotalTokens: total_tokens}
}

func TestTASend_StreamsPartialTextBeforeTheTurnEnds(t *testing.T) {
	nvimtest.RegisterProvider(t, slow(42, 150*time.Millisecond, "one ", "two ", "three"))
	config := nvimtest.Config()
	config.Now = fixed_clock("2026-10-06T14:32:00Z")
	harness := nvimtest.Start(t, config)

	harness.Command("TA foo")
	harness.SetText("## user\n\nhello\n")
	harness.Command("TASend")

	harness.WaitFor("partial text", func() bool { return strings.Contains(harness.Text(), "one") })
	if text := harness.Text(); strings.Contains(text, "three") {
		t.Fatalf("the whole reply landed at once:\n%s", text)
	}

	want := "## user · 2026-10-06T14:32:00Z\n\nhello\n\n" +
		"## assistant · fake-model · 2026-10-06T14:32:00Z · 42 tokens\n\none two three\n\n" +
		"## user\n\n"
	harness.WaitFor("the reply on disk", func() bool { return harness.ReadFile(SESSION) == want })
	if got := harness.Text(); got != want {
		t.Fatalf("buffer\nwant %q\ngot  %q", want, got)
	}
}
