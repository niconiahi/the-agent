package chat

import "github.com/charmbracelet/lipgloss"

var (
	USER_STYLE         = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	ASSISTANT_STYLE    = lipgloss.NewStyle()
	THINKING_STYLE     = lipgloss.NewStyle().Italic(true).Faint(true)
	TOOL_RUNNING_STYLE = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	TOOL_DONE_STYLE    = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	TOOL_ERROR_STYLE   = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	ERROR_STYLE        = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("1"))
	STATUS_BAR_STYLE   = lipgloss.NewStyle().Background(lipgloss.Color("236")).Foreground(lipgloss.Color("252")).Padding(0, 1)
)
