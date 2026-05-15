package chat

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type chat_entry struct {
	role    string
	content string
	detail  string
}

type tool_status struct {
	tool_name string
	detail    string
	state     string
}

func render_chat(history []chat_entry, streaming_text string, thinking_text string, pending_tools map[string]tool_status, error_message string) string {
	var sections []string

	for _, entry := range history {
		switch entry.role {
		case "user":
			sections = append(sections, USER_STYLE.Render("> "+entry.content))
		case "assistant":
			sections = append(sections, ASSISTANT_STYLE.Render(entry.content))
		case "thinking":
			sections = append(sections, THINKING_STYLE.Render(entry.content))
		case "tool":
			sections = append(sections, render_tool_line(entry.content, entry.detail, "done", false))
		case "tool_error":
			sections = append(sections, render_tool_line(entry.content, entry.detail, "error", true))
		}
	}

	if thinking_text != "" {
		sections = append(sections, THINKING_STYLE.Render(thinking_text))
	}

	if streaming_text != "" {
		sections = append(sections, ASSISTANT_STYLE.Render(streaming_text))
	}

	for _, tool := range pending_tools {
		sections = append(sections, render_tool_line(tool.tool_name, tool.detail, tool.state, false))
	}

	if error_message != "" {
		sections = append(sections, ERROR_STYLE.Render("error: "+error_message))
	}

	return strings.Join(sections, "\n\n")
}

func render_tool_line(tool_name string, detail string, state string, is_error bool) string {
	label := tool_name
	if detail != "" {
		label = tool_name + " " + detail
	}
	switch state {
	case "running":
		return TOOL_RUNNING_STYLE.Render(fmt.Sprintf("[tool: %s] running...", label))
	case "done":
		return TOOL_DONE_STYLE.Render(fmt.Sprintf("[tool: %s] done", label))
	case "error":
		return TOOL_ERROR_STYLE.Render(fmt.Sprintf("[tool: %s] error", label))
	}
	return ""
}

func tool_detail(tool_name string, arguments map[string]interface{}) string {
	switch tool_name {
	case "read":
		if path, ok := arguments["path"].(string); ok {
			return path
		}
	case "bash":
		if command, ok := arguments["command"].(string); ok {
			if len(command) > 60 {
				command = command[:60] + "..."
			}
			return command
		}
	case "write", "edit":
		if path, ok := arguments["path"].(string); ok {
			return path
		}
	case "grep":
		if pattern, ok := arguments["pattern"].(string); ok {
			return pattern
		}
	case "find":
		if pattern, ok := arguments["pattern"].(string); ok {
			return pattern
		}
	case "ls":
		if path, ok := arguments["path"].(string); ok {
			return path
		}
	}
	return ""
}

func render_status_bar(model_name string, is_streaming bool, total_tokens int, width int) string {
	model_section := STATUS_BAR_STYLE.Render(model_name)

	streaming_indicator := ""
	if is_streaming {
		streaming_indicator = STATUS_BAR_STYLE.Foreground(lipgloss.Color("3")).Render(" streaming...")
	}

	token_text := ""
	if total_tokens > 0 {
		token_text = fmt.Sprintf("%dk tokens", total_tokens/1000)
	}
	token_section := STATUS_BAR_STYLE.Render(token_text)

	spacer_width := width -
		lipgloss.Width(model_section) -
		lipgloss.Width(streaming_indicator) -
		lipgloss.Width(token_section)

	if spacer_width < 0 {
		spacer_width = 0
	}

	spacer := STATUS_BAR_STYLE.Render(strings.Repeat(" ", spacer_width))

	return lipgloss.JoinHorizontal(
		lipgloss.Top,
		model_section,
		streaming_indicator,
		spacer,
		token_section,
	)
}
