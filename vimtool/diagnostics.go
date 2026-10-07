package vimtool

import (
	"fmt"
	"strings"
	"time"
)

const DIAGNOSTICS_WAIT = 500 * time.Millisecond

func report(diagnostics []diagnostic) string {
	if len(diagnostics) == 0 {
		return ""
	}
	var builder strings.Builder
	builder.WriteString("\nDiagnostics in the edited region:\n")
	for _, found := range diagnostics {
		fmt.Fprintf(&builder, "%d:%d %s: %s", found.Line, found.Column, found.Severity, found.Message)
		if found.Source != "" {
			fmt.Fprintf(&builder, " (%s)", found.Source)
		}
		builder.WriteString("\n")
	}
	return builder.String()
}
