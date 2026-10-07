package integration_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/niconiahi/the-agent/nvim/nvimtest"
	"github.com/niconiahi/the-agent/setup"
)

func TestTA_InAProjectThatIsNotSetUpSaysSoAndCreatesNoSession(t *testing.T) {
	config := nvimtest.Config()
	config.Sandbox = setup.Ready
	harness := nvimtest.Start(t, config)

	error := harness.CommandError("TA foo")

	if error == nil || !strings.Contains(error.Error(), "sudo the-agent setup") {
		t.Fatalf("want :TA to tell me to run sudo the-agent setup, got %v", error)
	}
	if _, error := os.Stat(filepath.Join(harness.Dir, ".the-agent")); !errors.Is(error, os.ErrNotExist) {
		t.Fatalf("a project that is not set up must get no .the-agent folder: %v", error)
	}
}

func TestTA_CreatesSessionFileAndOpensIt(t *testing.T) {
	config := nvimtest.Config()
	config.Now = fixed_clock("2026-10-06T14:32:00Z")
	harness := nvimtest.Start(t, config)

	harness.Command("TA foo")

	path := filepath.Join(harness.Dir, ".the-agent", "sessions", "foo", "session.md")
	if _, error := os.Stat(path); error != nil {
		t.Fatalf("session file was not created: %v", error)
	}
	if got := harness.BufferName(); got != path {
		t.Fatalf("want current buffer %q, got %q", path, got)
	}
	if got, want := harness.Text(), "[system_prompt.md](../../system_prompt.md)\n\ncreated · 2026-10-06T14:32:00Z\n\n## user\n\n"; got != want {
		t.Fatalf("want new session %q, got %q", want, got)
	}
}

func TestTA_StaysInTheProjectAfterNeovimChangesDirectory(t *testing.T) {
	harness := nvimtest.Start(t, nvimtest.Config())
	other := filepath.Join(harness.Dir, "elsewhere")
	if error := os.Mkdir(other, 0o755); error != nil {
		t.Fatal(error)
	}

	harness.Command("cd " + other)
	harness.Command("TA foo")

	want := filepath.Join(harness.Dir, ".the-agent", "sessions", "foo", "session.md")
	if got := harness.BufferName(); got != want {
		t.Fatalf("want the session in the project %q, got %q", want, got)
	}
	if _, error := os.Stat(filepath.Join(other, ".the-agent")); !os.IsNotExist(error) {
		t.Fatalf("the session went to Neovim's new directory: %v", error)
	}
}

func TestTA_SeedsSystemPromptOnce(t *testing.T) {
	config := nvimtest.Config()
	harness := nvimtest.Start(t, config)

	harness.Command("TA foo")
	seeded := harness.ReadFile(".the-agent/system_prompt.md")
	if !strings.HasPrefix(seeded, config.SystemPrompt+"\n") {
		t.Fatalf("system_prompt.md not seeded: %q", seeded)
	}
	if !strings.Contains(seeded, "AGENTS.md") || !strings.Contains(seeded, "skills") {
		t.Fatalf("the seed must say where AGENTS.md and the skill list go: %q", seeded)
	}

	harness.WriteFile(".the-agent/system_prompt.md", "my rules\n")
	harness.Command("TA bar")
	if got := harness.ReadFile(".the-agent/system_prompt.md"); got != "my rules\n" {
		t.Fatalf("an existing system_prompt.md must be kept: %q", got)
	}
}

func TestTA_OpensExistingSession(t *testing.T) {
	harness := nvimtest.Start(t, nvimtest.Config())
	existing := "## user · 2026-10-06T14:32:00Z\n\nalready here\n"
	harness.WriteFile(".the-agent/sessions/foo/session.md", existing)

	harness.Command("TA foo")

	if got := harness.Text(); got != existing {
		t.Fatalf("want existing contents %q, got %q", existing, got)
	}
	if got := harness.ReadFile(".the-agent/sessions/foo/session.md"); got != existing {
		t.Fatalf("existing file was modified: %q", got)
	}
}

func TestTA_SlashInNameBecomesDash(t *testing.T) {
	harness := nvimtest.Start(t, nvimtest.Config())

	harness.Command("TA feature/auth")

	path := filepath.Join(harness.Dir, ".the-agent", "sessions", "feature-auth", "session.md")
	if got := harness.BufferName(); got != path {
		t.Fatalf("want current buffer %q, got %q", path, got)
	}
	if _, error := os.Stat(filepath.Join(harness.Dir, ".the-agent", "sessions", "feature")); !os.IsNotExist(error) {
		t.Fatalf("a nested folder must not be created")
	}
}
