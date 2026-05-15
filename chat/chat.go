package chat

import (
	"context"
	"os"
	"strings"
	"time"


	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/niconiahi/the-agent/message"
	"github.com/niconiahi/the-agent/orchestrator"
)

const (
	TEXTAREA_HEIGHT   = 3
	STATUS_BAR_HEIGHT = 1
)

type Model struct {
	agent          *orchestrator.Agent
	viewport       viewport.Model
	textarea       textarea.Model
	width          int
	height         int
	chat_history   []chat_entry
	streaming_text string
	thinking_text  string
	is_streaming   bool
	abort_count    int
	pending_tools  map[string]tool_status
	total_tokens   int
	model_name     string
	ready          bool
	error_message  string
	VerboseFile    *os.File
}

func New(agent *orchestrator.Agent, model_name string) Model {
	text_area := textarea.New()
	text_area.Placeholder = "Send a message..."
	text_area.Focus()
	text_area.SetHeight(TEXTAREA_HEIGHT)
	text_area.ShowLineNumbers = false
	text_area.CharLimit = 0

	return Model{
		agent:          agent,
		textarea:       text_area,
		viewport:       viewport.New(0, 0),
		pending_tools: make(map[string]tool_status),
		model_name:    model_name,
	}
}

func (model Model) Init() tea.Cmd {
	return textarea.Blink
}

func (model Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var commands []tea.Cmd

	switch typed := msg.(type) {
	case tea.WindowSizeMsg:
		model.width = typed.Width
		model.height = typed.Height
		viewport_height := typed.Height - TEXTAREA_HEIGHT - STATUS_BAR_HEIGHT - 1
		if viewport_height < 1 {
			viewport_height = 1
		}
		model.viewport.Width = typed.Width
		model.viewport.Height = viewport_height
		model.textarea.SetWidth(typed.Width)
		model.ready = true
		return model, nil

	case tea.KeyMsg:
		return model.handle_key(typed)

	case AgentStartMsg:
		model.is_streaming = true
		model.streaming_text = ""
		model.thinking_text = ""
		model.abort_count = 0

	case TextDeltaMsg:
		model.streaming_text += typed.Delta
		model.viewport.SetContent(render_chat(model.chat_history, model.streaming_text, model.thinking_text, model.pending_tools, model.error_message))
		model.viewport.GotoBottom()

	case ThinkingDeltaMsg:
		model.thinking_text += typed.Delta
		model.viewport.SetContent(render_chat(model.chat_history, model.streaming_text, model.thinking_text, model.pending_tools, model.error_message))
		model.viewport.GotoBottom()

	case ToolExecStartMsg:
		model.pending_tools[typed.ToolCallID] = tool_status{
			tool_name: typed.ToolName,
			detail:    tool_detail(typed.ToolName, typed.Arguments),
			state:     "running",
		}
		model.viewport.SetContent(render_chat(model.chat_history, model.streaming_text, model.thinking_text, model.pending_tools, model.error_message))
		model.viewport.GotoBottom()

	case ToolExecEndMsg:
		if tool, exists := model.pending_tools[typed.ToolCallID]; exists {
			tool.state = "done"
			if typed.IsError {
				tool.state = "error"
			}
			model.pending_tools[typed.ToolCallID] = tool
		}
		model.viewport.SetContent(render_chat(model.chat_history, model.streaming_text, model.thinking_text, model.pending_tools, model.error_message))

	case MessageEndMsg:
		model.finalize_message(typed.Message)
		model.viewport.SetContent(render_chat(model.chat_history, model.streaming_text, model.thinking_text, model.pending_tools, model.error_message))
		model.viewport.GotoBottom()

	case AgentEndMsg:
		model.is_streaming = false
		model.finalize_remaining()
		model.pending_tools = make(map[string]tool_status)
		model.viewport.SetContent(render_chat(model.chat_history, "", "", model.pending_tools, model.error_message))
		model.viewport.GotoBottom()

	case PromptDoneMsg:
		if typed.Error != nil {
			model.error_message = typed.Error.Error()
			model.is_streaming = false
			model.viewport.SetContent(render_chat(model.chat_history, "", "", model.pending_tools, model.error_message))
			model.viewport.GotoBottom()
		}
	}

	var viewport_cmd tea.Cmd
	model.viewport, viewport_cmd = model.viewport.Update(msg)
	if viewport_cmd != nil {
		commands = append(commands, viewport_cmd)
	}

	var textarea_cmd tea.Cmd
	model.textarea, textarea_cmd = model.textarea.Update(msg)
	if textarea_cmd != nil {
		commands = append(commands, textarea_cmd)
	}

	return model, tea.Batch(commands...)
}

func (model Model) View() string {
	if !model.ready {
		return "\n  Initializing..."
	}

	status := render_status_bar(model.model_name, model.is_streaming, model.total_tokens, model.width)

	return lipgloss.JoinVertical(
		lipgloss.Left,
		model.viewport.View(),
		status,
		model.textarea.View(),
	)
}

func (model Model) handle_key(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.Type {
	case tea.KeyCtrlC:
		if model.is_streaming {
			model.abort_count++
			if model.abort_count == 1 {
				model.agent.Abort()
				return model, nil
			}
		}
		return model, tea.Quit

	case tea.KeyEsc:
		if model.is_streaming {
			model.agent.Abort()
			return model, nil
		}
		return model, tea.Quit

	case tea.KeyEnter:
		if key.Alt {
			break
		}
		text := strings.TrimSpace(model.textarea.Value())
		if text == "" {
			return model, nil
		}
		model.textarea.Reset()

		model.error_message = ""
		user_entry := chat_entry{role: "user", content: text}
		model.chat_history = append(model.chat_history, user_entry)
		model.viewport.SetContent(render_chat(model.chat_history, "", "", model.pending_tools, model.error_message))
		model.viewport.GotoBottom()

		if model.is_streaming {
			model.agent.Steer(message.UserMessage{
				Content:   []message.Content{message.TextContent{Text: text}},
				Timestamp: time.Now(),
			})
			return model, nil
		}

		agent := model.agent
		return model, func() tea.Msg {
			error := agent.PromptText(context.Background(), text)
			return PromptDoneMsg{Error: error}
		}
	}

	var command tea.Cmd
	model.textarea, command = model.textarea.Update(key)
	return model, command
}

func (model *Model) finalize_message(final_message message.Message) {
	if len(model.thinking_text) > 0 {
		model.chat_history = append(model.chat_history, chat_entry{
			role:    "thinking",
			content: model.thinking_text,
		})
		model.thinking_text = ""
	}

	if len(model.streaming_text) > 0 {
		model.chat_history = append(model.chat_history, chat_entry{
			role:    "assistant",
			content: model.streaming_text,
		})
		model.streaming_text = ""
	}

	for id, tool := range model.pending_tools {
		role := "tool"
		if tool.state == "error" {
			role = "tool_error"
		}
		model.chat_history = append(model.chat_history, chat_entry{
			role:    role,
			content: tool.tool_name,
			detail:  tool.detail,
		})
		delete(model.pending_tools, id)
	}

	if assistant_message, ok := final_message.(message.AssistantMessage); ok {
		model.total_tokens += assistant_message.Usage.TotalTokens
	}
}

func (model *Model) finalize_remaining() {
	if len(model.thinking_text) > 0 {
		model.chat_history = append(model.chat_history, chat_entry{
			role:    "thinking",
			content: model.thinking_text,
		})
		model.thinking_text = ""
	}

	if len(model.streaming_text) > 0 {
		model.chat_history = append(model.chat_history, chat_entry{
			role:    "assistant",
			content: model.streaming_text,
		})
		model.streaming_text = ""
	}
}
