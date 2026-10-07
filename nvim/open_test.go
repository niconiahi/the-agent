package nvim_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/niconiahi/the-agent/nvim/nvimtest"
)

func TestTA_CreatesSessionFileAndOpensIt(t *testing.T) {
	harness := nvimtest.Start(t, nvimtest.Config())

	harness.Command("TA foo")

	path := filepath.Join(harness.Dir, ".the-agent", "sessions", "foo", "session.md")
	if _, error := os.Stat(path); error != nil {
		t.Fatalf("session file was not created: %v", error)
	}
	if got := harness.BufferName(); got != path {
		t.Fatalf("want current buffer %q, got %q", path, got)
	}
	if got, want := harness.Text(), "## user\n\n"; got != want {
		t.Fatalf("want new session %q, got %q", want, got)
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
