package session_test

import (
	"testing"

	"github.com/niconiahi/the-agent/session"
)

func TestCeiling_DefaultsTo200k(t *testing.T) {
	if got := session.Ceiling(0, 262144); got != 200000 {
		t.Fatalf("want 200000, got %d", got)
	}
}

func TestCeiling_UsesConfiguredValue(t *testing.T) {
	if got := session.Ceiling(50000, 262144); got != 50000 {
		t.Fatalf("want 50000, got %d", got)
	}
}

func TestCeiling_NeverAboveContextWindow(t *testing.T) {
	if got := session.Ceiling(500000, 262144); got != 262144 {
		t.Fatalf("want 262144, got %d", got)
	}
	if got := session.Ceiling(0, 128000); got != 128000 {
		t.Fatalf("default must clamp too: want 128000, got %d", got)
	}
}
