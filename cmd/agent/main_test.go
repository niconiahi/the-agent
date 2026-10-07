package main

import (
	"runtime"
	"testing"
)

func tool_names(t *testing.T) map[string]bool {
	names := map[string]bool{}
	for _, current := range default_tools(nil, t.TempDir()) {
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

func TestDefaultTools_OfferBashWriteOnMacOS(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("bash_write clones with clonefile on macOS only")
	}

	if names := tool_names(t); !names["bash_write"] {
		t.Fatalf("want bash_write among %v", names)
	}
}
