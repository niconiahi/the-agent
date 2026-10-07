package integration_test

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/niconiahi/the-agent/nvim"
	"github.com/niconiahi/the-agent/nvim/nvimtest"
)

func send_in(harness *nvimtest.Harness, session string, text string) {
	harness.T.Helper()
	harness.Command("TA " + session)
	harness.SetText("## user\n\n" + text + "\n")
	harness.Command("TASend")
}

func session_on_disk(harness *nvimtest.Harness, session string) string {
	contents, _ := os.ReadFile(nvim.SessionPath(harness.Dir, session))
	return string(contents)
}

func session_modifiable(harness *nvimtest.Harness, session string) bool {
	harness.T.Helper()
	var value bool
	if error := harness.Nvim.ExecLua(`return vim.bo[vim.fn.bufnr(...)].modifiable`, &value, nvim.SessionPath(harness.Dir, session)); error != nil {
		harness.T.Fatal(error)
	}
	return value
}

func TestTASend_TwoSessionsAtOnceEachStreamOnlyIntoTheirOwnFile(t *testing.T) {
	provider := nvimtest.RegisterProvider(t)
	provider.Script("alpha", slow(11, 60*time.Millisecond, "a1 ", "a2 ", "a3 ", "a4"))
	provider.Script("beta", slow(22, 60*time.Millisecond, "b1 ", "b2 ", "b3 ", "b4"))
	config := nvimtest.Config()
	config.Now = fixed_clock("2026-10-06T14:32:00Z")
	harness := nvimtest.Start(t, config)

	send_in(harness, "one", "alpha")
	send_in(harness, "two", "beta")

	if session_modifiable(harness, "one") || session_modifiable(harness, "two") {
		t.Fatal("both sessions should be locked while both turns run")
	}

	want_one := "## user · 2026-10-06T14:32:00Z\n\nalpha\n\n" +
		"## assistant · fake-model · 2026-10-06T14:32:00Z · 11 tokens\n\na1 a2 a3 a4\n\n" +
		"## user\n\n"
	want_two := "## user · 2026-10-06T14:32:00Z\n\nbeta\n\n" +
		"## assistant · fake-model · 2026-10-06T14:32:00Z · 22 tokens\n\nb1 b2 b3 b4\n\n" +
		"## user\n\n"
	harness.WaitFor("both replies on disk", func() bool {
		return session_on_disk(harness, "one") == want_one && session_on_disk(harness, "two") == want_two
	})
	if !session_modifiable(harness, "one") || !session_modifiable(harness, "two") {
		t.Fatal("a session stayed locked after its turn ended")
	}
}

func TestTAAbort_AbortsOnlyTheCurrentSession(t *testing.T) {
	provider := nvimtest.RegisterProvider(t)
	provider.Script("alpha", slow(11, 150*time.Millisecond, "a1 ", "a2 ", "a3"))
	provider.Script("beta", slow(22, 150*time.Millisecond, "b1 ", "b2 ", "b3"))
	config := nvimtest.Config()
	config.Now = fixed_clock("2026-10-06T14:32:00Z")
	harness := nvimtest.Start(t, config)

	send_in(harness, "one", "alpha")
	send_in(harness, "two", "beta")
	harness.Command("TA one")
	harness.WaitFor("partial text in one", func() bool { return strings.Contains(harness.Text(), "a1") })
	harness.Command("TAAbort")

	if !session_modifiable(harness, "one") {
		t.Fatal("the aborted session is still locked")
	}
	if session_modifiable(harness, "two") {
		t.Fatal(":TAAbort in one unlocked two")
	}
	if got := session_on_disk(harness, "one"); !strings.Contains(got, "· aborted\n") {
		t.Fatalf("one was not aborted:\n%s", got)
	}

	want_two := "## user · 2026-10-06T14:32:00Z\n\nbeta\n\n" +
		"## assistant · fake-model · 2026-10-06T14:32:00Z · 22 tokens\n\nb1 b2 b3\n\n" +
		"## user\n\n"
	harness.WaitFor("two finishing on its own", func() bool { return session_on_disk(harness, "two") == want_two })
}
