package integration_test

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

	harness.Command("normal! gg")
	harness.Command("normal! G")

	wait_turn_end(harness, "two")
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
	wait_turn_end(harness, "three")
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

	wait_turn_end(harness, "three")
	if line := eval_int(harness, `line(".")`); line != 1 {
		t.Fatalf("the cursor moved to line %d", line)
	}
	if top := eval_int(harness, `line("w0")`); top != 1 {
		t.Fatalf("the view scrolled to line %d", top)
	}
}

func wait_turn_end(harness *nvimtest.Harness, last_word string) {
	harness.T.Helper()
	harness.WaitFor("the turn to end", func() bool {
		return modifiable(harness) && strings.HasSuffix(on_disk(harness), last_word+"\n\n## user\n\n")
	})
}

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

func TestTAAbort_StopsTheTurnKeepsTheTextAndUnlocks(t *testing.T) {
	provider := nvimtest.RegisterProvider(t, slow(1, 300*time.Millisecond, "kept ", "lost"))
	config := nvimtest.Config()
	config.Now = fixed_clock("2026-10-06T14:32:00Z")
	harness := nvimtest.Start(t, config)

	harness.Command("TA foo")
	harness.SetText("## user\n\nhello\n")
	harness.Command("TASend")
	harness.WaitFor("partial text", func() bool { return strings.Contains(harness.Text(), "kept") })
	harness.Command("TAAbort")

	if !modifiable(harness) {
		t.Fatal("the session is still locked after :TAAbort")
	}
	want := "## user · 2026-10-06T14:32:00Z\n\nhello\n\n" +
		"## assistant · fake-model · 2026-10-06T14:32:00Z · aborted\n\nkept\n\n" +
		"## user\n\n"
	if got := harness.Text(); got != want {
		t.Fatalf("buffer\nwant %q\ngot  %q", want, got)
	}
	if got := on_disk(harness); got != want {
		t.Fatalf("disk\nwant %q\ngot  %q", want, got)
	}

	harness.SetText(want + "go on\n")
	if error := harness.CommandError("TASend"); error != nil {
		t.Fatalf("send after abort: %v", error)
	}
	harness.WaitFor("the second request", func() bool { return len(provider.Requests()) == 2 })
}

func TestTAAbort_RefusesWhenNoTurnIsRunning(t *testing.T) {
	nvimtest.RegisterProvider(t)
	harness := nvimtest.Start(t, nvimtest.Config())

	harness.Command("TA foo")
	error := harness.CommandError("TAAbort")

	if error == nil || !strings.Contains(error.Error(), "no turn is running") {
		t.Fatalf("want a 'no turn is running' error, got %v", error)
	}
}
