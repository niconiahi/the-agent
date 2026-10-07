package nvim_test

import (
	"strings"
	"testing"
	"time"

	"github.com/niconiahi/the-agent/message"
	"github.com/niconiahi/the-agent/nvim/nvimtest"
)

const SESSION = ".the-agent/sessions/foo/session.md"

func fixed_clock(value string) func() time.Time {
	parsed, _ := time.Parse(time.RFC3339, value)
	return func() time.Time { return parsed }
}

func TestTASend_WritesReplyUnderAssistantHeading(t *testing.T) {
	provider := nvimtest.RegisterProvider(t, nvimtest.Text("hi there", 1240))
	config := nvimtest.Config()
	config.Now = fixed_clock("2026-10-06T14:32:00Z")
	harness := nvimtest.Start(t, config)

	harness.Command("TA foo")
	harness.SetText("## user\n\nhello\n")
	harness.Command("TASend")

	want := "## user · 2026-10-06T14:32:00Z\n\nhello\n\n" +
		"## assistant · fake-model · 2026-10-06T14:32:00Z · 1,240 tokens\n\nhi there\n\n" +
		"## user\n\n"
	harness.WaitFor("the reply on disk", func() bool { return harness.ReadFile(SESSION) == want })
	if got := harness.Text(); got != want {
		t.Fatalf("buffer\nwant %q\ngot  %q", want, got)
	}

	requests := provider.Requests()
	if len(requests) != 1 {
		t.Fatalf("want 1 request, got %d", len(requests))
	}
	assert_texts(t, requests[0].Messages, "2026-10-06T14:32:00Z\n\nhello")
}

func TestTASend_SendsWholeSessionWithTimestamps(t *testing.T) {
	provider := nvimtest.RegisterProvider(t, nvimtest.Text("first", 10), nvimtest.Text("second", 20))
	config := nvimtest.Config()
	config.Now = fixed_clock("2026-10-06T14:32:00Z")
	harness := nvimtest.Start(t, config)

	harness.Command("TA foo")
	harness.SetText("## user\n\none\n")
	harness.Command("TASend")
	harness.WaitFor("first turn to end", func() bool { return modifiable(harness) && strings.Contains(harness.Text(), "first") })

	harness.SetText(harness.Text() + "two\n")
	harness.Command("TASend")
	harness.WaitFor("second turn to end", func() bool { return modifiable(harness) && strings.Contains(harness.Text(), "second") })

	requests := provider.Requests()
	if len(requests) != 2 {
		t.Fatalf("want 2 requests, got %d", len(requests))
	}
	assert_texts(t, requests[1].Messages,
		"2026-10-06T14:32:00Z\n\none",
		"fake-model · 2026-10-06T14:32:00Z · 10 tokens\n\nfirst",
		"2026-10-06T14:32:00Z\n\ntwo",
	)
}

func TestTASend_RefusesWithoutAUserMessage(t *testing.T) {
	provider := nvimtest.RegisterProvider(t)
	harness := nvimtest.Start(t, nvimtest.Config())

	harness.Command("TA foo")
	before := harness.Text()
	error := harness.CommandError("TASend")

	if error == nil || !strings.Contains(error.Error(), "## user") {
		t.Fatalf("want an error pointing at the ## user heading, got %v", error)
	}
	if len(provider.Requests()) != 0 {
		t.Fatal("nothing must be sent")
	}
	if got := harness.Text(); got != before {
		t.Fatalf("buffer changed: %q", got)
	}
}

func assert_texts(t *testing.T, messages []message.Message, want ...string) {
	t.Helper()
	if len(messages) != len(want) {
		t.Fatalf("want %d messages, got %d: %#v", len(want), len(messages), messages)
	}
	for index, value := range messages {
		var content []message.Content
		switch typed := value.(type) {
		case message.UserMessage:
			content = typed.Content
		case message.AssistantMessage:
			content = typed.Content
		case *message.AssistantMessage:
			content = typed.Content
		default:
			t.Fatalf("message %d: unexpected %T", index, value)
		}
		text, _ := content[0].(message.TextContent)
		if text.Text != want[index] {
			t.Errorf("message %d\nwant %q\ngot  %q", index, want[index], text.Text)
		}
	}
}
