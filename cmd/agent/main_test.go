package main

import (
	"runtime"
	"testing"
)

func tool_names(t *testing.T) map[string]bool {
	names := map[string]bool{}
	for _, current := range default_tools(nil, t.TempDir(), "/usr/local/bin/the-agent") {
		names[current.Name] = true
	}
	return names
}

func TestDefaultTools_RunCommandsThroughBashReadOnly(t *testing.T) {
	names := tool_names(t)

	if names["bash"] {
		t.Fatal("the bash tool must be gone")
	}
	if !names["bash_read"] {
		t.Fatalf("want bash_read among %v", names)
	}
}

func TestDefaultTools_OfferBashWriteOnMacOSAndLinux(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("bash_write is registered on macOS and Linux")
	}

	if names := tool_names(t); !names["bash_write"] {
		t.Fatalf("want bash_write among %v", names)
	}
}

func TestMaxDepth_ComesFromTheEnvironmentAndDefaultsWhenUnset(t *testing.T) {
	for value, want := range map[string]int{"": 0, "1": 1, " 5 ": 5} {
		if got, error := max_depth(value); error != nil || got != want {
			t.Errorf("max_depth(%q) = %d, %v; want %d", value, got, error, want)
		}
	}
	for _, value := range []string{"0", "-2", "three"} {
		want := `THE_AGENT_MAX_DEPTH must be a positive whole number, got "` + value + `"`
		if _, error := max_depth(value); error == nil || error.Error() != want {
			t.Errorf("max_depth(%q) error = %v, want %s", value, error, want)
		}
	}
}
