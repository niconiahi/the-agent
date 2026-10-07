package nvim_test

import (
	"os"
	"path/filepath"
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
	harness.WaitFor("the reply on disk", func() bool { return on_disk(harness) == want })
	if got := harness.Text(); got != want {
		t.Fatalf("buffer\nwant %q\ngot  %q", want, got)
	}
}

func TestTASend_BatchesDeltasInsteadOfOneEditPerDelta(t *testing.T) {
	deltas := []string{}
	for range 100 {
		deltas = append(deltas, "x")
	}
	nvimtest.RegisterProvider(t, slow(1, 2*time.Millisecond, deltas...))
	harness := nvimtest.Start(t, nvimtest.Config())

	harness.Command("TA foo")
	harness.SetText("## user\n\nhello\n")
	before := changedtick(harness)
	harness.Command("TASend")
	harness.WaitFor("the reply on disk", func() bool {
		return strings.Contains(on_disk(harness), strings.Repeat("x", 100))
	})

	// Every buffer edit bumps b:changedtick. One edit per delta would be
	// over 100; batching every ~40ms over ~200ms of streaming is a handful.
	if edits := changedtick(harness) - before; edits > 25 {
		t.Fatalf("100 deltas took %d buffer edits", edits)
	}
}

func TestTASend_LocksTheSessionDuringTheTurn(t *testing.T) {
	nvimtest.RegisterProvider(t, slow(1, 150*time.Millisecond, "one ", "two"))
	harness := nvimtest.Start(t, nvimtest.Config())

	harness.Command("TA foo")
	harness.SetText("## user\n\nhello\n")
	harness.Command("TASend")

	if modifiable(harness) {
		t.Fatal("the session is modifiable while the turn runs")
	}
	harness.WaitFor("partial text", func() bool { return strings.Contains(harness.Text(), "one") })
	if modifiable(harness) {
		t.Fatal("the session became modifiable while streaming")
	}
	// Reading and moving still work.
	harness.Command("normal! gg")
	harness.Command("normal! G")

	harness.WaitFor("the turn to end", func() bool { return strings.HasSuffix(on_disk(harness), "## user\n\n") })
	harness.WaitFor("the session to unlock", func() bool { return modifiable(harness) })
}

func TestTASend_FollowsTheStreamWhenTheCursorIsAtTheBottom(t *testing.T) {
	nvimtest.RegisterProvider(t, slow(1, 100*time.Millisecond, "one\n\n", "two\n\n", "three"))
	harness := nvimtest.Start(t, nvimtest.Config())

	harness.Command("TA foo")
	harness.SetText("## user\n\nhello\n")
	harness.Command("normal! G")
	harness.Command("TASend")

	harness.WaitFor("partial text", func() bool { return strings.Contains(harness.Text(), "one") })
	if line, last := eval_int(harness, `line(".")`), eval_int(harness, `line("$")`); line != last {
		t.Fatalf("mid-stream the cursor is on line %d of %d", line, last)
	}
	harness.WaitFor("the turn to end", func() bool { return strings.HasSuffix(on_disk(harness), "## user\n\n") })
	if line, last := eval_int(harness, `line(".")`), eval_int(harness, `line("$")`); line != last {
		t.Fatalf("after the turn the cursor is on line %d of %d", line, last)
	}
}

func TestTASend_LeavesTheViewAloneWhenTheCursorIsElsewhere(t *testing.T) {
	nvimtest.RegisterProvider(t, slow(1, 100*time.Millisecond, "one\n\n", "two\n\n", "three"))
	harness := nvimtest.Start(t, nvimtest.Config())

	harness.Command("TA foo")
	harness.SetText("## user\n\nhello\n")
	harness.Command("normal! gg")
	harness.Command("TASend")

	harness.WaitFor("the turn to end", func() bool { return strings.HasSuffix(on_disk(harness), "## user\n\n") })
	if line := eval_int(harness, `line(".")`); line != 1 {
		t.Fatalf("the cursor moved to line %d", line)
	}
	if top := eval_int(harness, `line("w0")`); top != 1 {
		t.Fatalf("the view scrolled to line %d", top)
	}
}

// on_disk is the session file, or "" while :write has it renamed away
// (Neovim's writebackup), so it can be polled.
func on_disk(harness *nvimtest.Harness) string {
	contents, _ := os.ReadFile(filepath.Join(harness.Dir, SESSION))
	return string(contents)
}

func eval_int(harness *nvimtest.Harness, expression string) int {
	harness.T.Helper()
	var value int
	if error := harness.Nvim.Eval(expression, &value); error != nil {
		harness.T.Fatal(error)
	}
	return value
}

func modifiable(harness *nvimtest.Harness) bool {
	harness.T.Helper()
	var value bool
	if error := harness.Nvim.Eval("&modifiable", &value); error != nil {
		harness.T.Fatal(error)
	}
	return value
}

func changedtick(harness *nvimtest.Harness) int {
	harness.T.Helper()
	var tick int
	if error := harness.Nvim.Eval("b:changedtick", &tick); error != nil {
		harness.T.Fatal(error)
	}
	return tick
}
