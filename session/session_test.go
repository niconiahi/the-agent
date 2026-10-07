package session_test

import (
	"testing"
	"time"

	"github.com/niconiahi/the-agent/message"
	"github.com/niconiahi/the-agent/session"
)

const text_only_session = `## user · 2026-10-06T14:32:00Z

hello, can you
explain this?

## assistant · kimi-k2.5 · 2026-10-06T14:33:00Z · 1,240 tokens

Sure. Here is a heading inside the reply:

## Not a turn

` + "```go\n## user inside a code block\n```" + `

## user · 2026-10-06T14:40:00Z

thanks
`

func TestRoundTrip_TextOnlyIsIdentity(t *testing.T) {
	cases := map[string]string{
		"full":          text_only_session,
		"empty":         "",
		"bare heading":  "## user\n\n",
		"no final eol":  "## user · 2026-10-06T14:32:00Z\n\nhi",
		"preamble kept": "some preamble\n\n## user\n\nhi\n",
	}
	for name, text := range cases {
		t.Run(name, func(t *testing.T) {
			parsed, error := session.Parse(text)
			if error != nil {
				t.Fatalf("parse: %v", error)
			}
			if got := parsed.Render(); got != text {
				t.Fatalf("round trip changed the file\nwant:\n%q\ngot:\n%q", text, got)
			}
		})
	}
}

func TestParse_ReadsRoleAndSendsTimestampsInContent(t *testing.T) {
	parsed, error := session.Parse(text_only_session)
	if error != nil {
		t.Fatalf("parse: %v", error)
	}

	messages := parsed.Messages()
	if len(messages) != 3 {
		t.Fatalf("want 3 messages, got %d", len(messages))
	}

	want := []struct {
		role string
		text string
	}{
		{"user", "2026-10-06T14:32:00Z\n\nhello, can you\nexplain this?"},
		{"assistant", "kimi-k2.5 · 2026-10-06T14:33:00Z · 1,240 tokens\n\nSure. Here is a heading inside the reply:\n\n## Not a turn\n\n```go\n## user inside a code block\n```"},
		{"user", "2026-10-06T14:40:00Z\n\nthanks"},
	}
	for index, expected := range want {
		role, text := role_and_text(t, messages[index])
		if role != expected.role {
			t.Errorf("message %d: want role %q, got %q", index, expected.role, role)
		}
		if text != expected.text {
			t.Errorf("message %d: want text\n%q\ngot\n%q", index, expected.text, text)
		}
	}
}

func TestParse_FreeFormHeadingIsIgnoredExceptRole(t *testing.T) {
	parsed, error := session.Parse("## assistant whatever I typed here\n\nok\n## user\n\nnext\n")
	if error != nil {
		t.Fatalf("parse: %v", error)
	}
	messages := parsed.Messages()
	if len(messages) != 2 {
		t.Fatalf("want 2 messages, got %d", len(messages))
	}
	role, text := role_and_text(t, messages[0])
	if role != "assistant" || text != "whatever I typed here\n\nok" {
		t.Fatalf("got %s %q", role, text)
	}
	role, text = role_and_text(t, messages[1])
	if role != "user" || text != "next" {
		t.Fatalf("got %s %q", role, text)
	}
}

func TestAppend_RendersAssistantHeadingAndNextUserHeading(t *testing.T) {
	parsed, error := session.Parse("## user · 2026-10-06T14:32:00Z\n\nhi\n")
	if error != nil {
		t.Fatalf("parse: %v", error)
	}
	parsed.AppendAssistant("kimi-k2.5", mustTime(t, "2026-10-06T14:33:05Z"), 1240, "hello there")
	parsed.AppendUser()

	want := "## user · 2026-10-06T14:32:00Z\n\nhi\n\n" +
		"## assistant · kimi-k2.5 · 2026-10-06T14:33:05Z · 1,240 tokens\n\nhello there\n\n" +
		"## user\n\n"
	if got := parsed.Render(); got != want {
		t.Fatalf("want\n%q\ngot\n%q", want, got)
	}
}

func TestStampLastUser_AddsTimestampOnlyToBareHeading(t *testing.T) {
	parsed, _ := session.Parse("## user\n\nhi\n")
	if !parsed.StampLastUser(mustTime(t, "2026-10-06T14:32:00Z")) {
		t.Fatal("expected bare heading to be stamped")
	}
	if got, want := parsed.Render(), "## user · 2026-10-06T14:32:00Z\n\nhi\n"; got != want {
		t.Fatalf("want %q got %q", want, got)
	}
	if parsed.StampLastUser(mustTime(t, "2026-10-06T15:00:00Z")) {
		t.Fatal("an already stamped heading must not change")
	}
}

func role_and_text(t *testing.T, value message.Message) (string, string) {
	t.Helper()
	switch typed := value.(type) {
	case message.UserMessage:
		return "user", only_text(t, typed.Content)
	case message.AssistantMessage:
		return "assistant", only_text(t, typed.Content)
	default:
		t.Fatalf("unexpected message type %T", value)
		return "", ""
	}
}

func only_text(t *testing.T, content []message.Content) string {
	t.Helper()
	if len(content) != 1 {
		t.Fatalf("want 1 content block, got %d", len(content))
	}
	text, ok := content[0].(message.TextContent)
	if !ok {
		t.Fatalf("want text content, got %T", content[0])
	}
	return text.Text
}

func mustTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, error := time.Parse(time.RFC3339, value)
	if error != nil {
		t.Fatal(error)
	}
	return parsed
}
