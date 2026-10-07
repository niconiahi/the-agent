package nvim_test

import (
	"strings"
	"testing"

	"github.com/niconiahi/the-agent/nvim/nvimtest"
)

func TestTASend_SendsLinkedSystemPrompt(t *testing.T) {
	provider := nvimtest.RegisterProvider(t, nvimtest.Text("ok", 10))
	config := nvimtest.Config()
	config.Now = fixed_clock("2026-10-06T14:32:00Z")
	harness := nvimtest.Start(t, config)

	harness.Command("TA foo")
	harness.WriteFile(".the-agent/system_prompt.md", "Be terse.\nAGENTS.md says: use tabs.\n")
	harness.SetText(harness.Text() + "hello\n")
	harness.Command("TASend")
	harness.WaitFor("reply", func() bool { return strings.Contains(harness.Text(), "ok") })

	requests := provider.Requests()
	if len(requests) != 1 {
		t.Fatalf("want 1 request, got %d", len(requests))
	}
	if got, want := requests[0].SystemPrompt, "Be terse.\nAGENTS.md says: use tabs."; got != want {
		t.Fatalf("system prompt\nwant %q\ngot  %q", want, got)
	}

	assert_texts(t, requests[0].Messages, "2026-10-06T14:32:00Z\n\nhello")
}

func TestTASend_WithoutLinkSendsNoSystemPrompt(t *testing.T) {
	provider := nvimtest.RegisterProvider(t, nvimtest.Text("ok", 10))
	harness := nvimtest.Start(t, nvimtest.Config())

	harness.Command("TA foo")
	harness.SetText("## user\n\nhello\n")
	harness.Command("TASend")
	harness.WaitFor("reply", func() bool { return strings.Contains(harness.Text(), "ok") })

	if got := provider.Requests()[0].SystemPrompt; got != "" {
		t.Fatalf("the file is the context: no link, no system prompt; got %q", got)
	}
}

func TestTASend_RefusesWhenLinkedSystemPromptIsMissing(t *testing.T) {
	provider := nvimtest.RegisterProvider(t)
	harness := nvimtest.Start(t, nvimtest.Config())

	harness.Command("TA foo")
	harness.SetText("[rules](../../missing.md)\n\n## user\n\nhello\n")
	error := harness.CommandError("TASend")

	if error == nil || !strings.Contains(error.Error(), "missing.md") {
		t.Fatalf("want an error naming the missing file, got %v", error)
	}
	if len(provider.Requests()) != 0 {
		t.Fatal("nothing must be sent")
	}
}
