package session_test

import (
	"testing"

	"github.com/niconiahi/the-agent/session"
)

func TestNew_LinksTheSystemPromptAndStampsTheCreation(t *testing.T) {
	text := session.New(must_time(t, "2026-10-06T14:32:00Z"))

	want := "[system_prompt.md](../../system_prompt.md)\n\ncreated · 2026-10-06T14:32:00Z\n\n## user\n\n"
	if text != want {
		t.Fatalf("want %q, got %q", want, text)
	}
	parsed, error := session.Parse(text)
	if error != nil {
		t.Fatal(error)
	}
	if got := parsed.Render(); got != text {
		t.Fatalf("round trip changed the file: %q", got)
	}
	if link, ok := parsed.SystemPromptLink(); !ok || link != "../../system_prompt.md" {
		t.Fatalf("link: got %q %v", link, ok)
	}
}

func TestSystemPrompt_EndsWithTheCreationLineTheFileShows(t *testing.T) {
	parsed, _ := session.Parse(session.New(must_time(t, "2026-10-06T14:32:00Z")))

	if got, want := parsed.SystemPrompt("Be terse."), "Be terse.\n\ncreated · 2026-10-06T14:32:00Z"; got != want {
		t.Fatalf("want %q, got %q", want, got)
	}
	if got, want := parsed.SystemPrompt(""), "created · 2026-10-06T14:32:00Z"; got != want {
		t.Fatalf("without a linked prompt: want %q, got %q", want, got)
	}
}

func TestSystemPrompt_IsOnlyTheLinkedFileWhenTheCreationLineIsGone(t *testing.T) {
	parsed, _ := session.Parse("[system_prompt.md](../../system_prompt.md)\n\n## user\n\nhi\n")

	if got := parsed.SystemPrompt("Be terse."); got != "Be terse." {
		t.Fatalf("got %q", got)
	}
}
