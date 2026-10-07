package nvim_test

import (
	"strings"
	"testing"

	"github.com/niconiahi/the-agent/nvim/nvimtest"
	"github.com/niconiahi/the-agent/session"
)

func TestTASend_RefusesAboveCeilingAndReportsCount(t *testing.T) {
	provider := nvimtest.RegisterProvider(t, nvimtest.Text("never", 1))
	config := nvimtest.Config()
	config.Now = fixed_clock("2026-10-06T14:32:00Z")
	harness := nvimtest.Start(t, config)
	harness.Setup(`{ ceiling = 5 }`)

	harness.Command("TA foo")
	unsent := session.SYSTEM_PROMPT_LINK + "\n\n## user\n\nhello\n"
	harness.SetText(unsent)
	error := harness.CommandError("TASend")

	if error == nil || !strings.Contains(error.Error(), "12 tokens") || !strings.Contains(error.Error(), "ceiling of 5") {
		t.Fatalf("want a refusal with the count and the ceiling, got %v", error)
	}
	if len(provider.Requests()) != 0 {
		t.Fatal("nothing must be sent above the ceiling")
	}
	if got := harness.Text(); got != unsent {
		t.Fatalf("a refused send must leave the buffer alone: %q", got)
	}
}

func TestTASend_SendsAtOrBelowCeiling(t *testing.T) {
	provider := nvimtest.RegisterProvider(t, nvimtest.Text("ok", 1))
	config := nvimtest.Config()
	config.Now = fixed_clock("2026-10-06T14:32:00Z")
	harness := nvimtest.Start(t, config)
	harness.Setup(`{ ceiling = 12 }`)

	harness.Command("TA foo")
	harness.SetText(session.SYSTEM_PROMPT_LINK + "\n\n## user\n\nhello\n")
	harness.Command("TASend")
	harness.WaitFor("reply", func() bool { return strings.Contains(harness.Text(), "ok") })

	if len(provider.Requests()) != 1 {
		t.Fatal("a session at the ceiling must be sent")
	}
}
