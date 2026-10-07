package main

import "testing"

func TestDefaultTools_RunCommandsThroughBashReadOnly(t *testing.T) {
	names := map[string]bool{}
	for _, current := range default_tools(nil, t.TempDir()) {
		names[current.Name] = true
	}

	if names["bash"] {
		t.Fatal("the bash tool must be gone")
	}
	if !names["bash_read"] {
		t.Fatalf("want bash_read among %v", names)
	}
}
