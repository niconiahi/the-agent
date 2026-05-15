package chat

import (
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/niconiahi/the-agent/orchestrator"
	"github.com/niconiahi/the-agent/sender"
)

func StartEventBridge(agent *orchestrator.Agent, program *tea.Program, verbose_file *os.File) func() {
	return agent.Subscribe(func(event orchestrator.AgentEvent) {
		if verbose_file != nil {
			fmt.Fprintf(verbose_file, "[%s] %s %T\n", time.Now().Format(time.RFC3339Nano), event.AgentEventType(), event)
		}

		switch typed := event.(type) {
		case orchestrator.AgentStartEvent:
			program.Send(AgentStartMsg{})

		case orchestrator.AgentEndEvent:
			program.Send(AgentEndMsg{Messages: typed.Messages})

		case orchestrator.MessageUpdateEvent:
			bridge_sender_event(typed, program)

		case orchestrator.MessageEndEvent:
			program.Send(MessageEndMsg{Message: typed.Message})

		case orchestrator.ToolExecutionStartEvent:
			program.Send(ToolExecStartMsg{
				ToolCallID: typed.ToolCallID,
				ToolName:   typed.ToolName,
				Arguments:  typed.Arguments,
			})

		case orchestrator.ToolExecutionEndEvent:
			program.Send(ToolExecEndMsg{
				ToolCallID: typed.ToolCallID,
				ToolName:   typed.ToolName,
				IsError:    typed.IsError,
			})
		}
	})
}

func bridge_sender_event(event orchestrator.MessageUpdateEvent, program *tea.Program) {
	switch typed := event.SenderEvent.(type) {
	case sender.EventTextDelta:
		program.Send(TextDeltaMsg{Delta: typed.Delta})
	case sender.EventThinkingDelta:
		program.Send(ThinkingDeltaMsg{Delta: typed.Delta})
	case sender.EventToolCallEnd:
		program.Send(ToolCallEndMsg{ToolCall: typed.ToolCall})
	}
}
