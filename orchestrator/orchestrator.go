package orchestrator

import (
	"context"
	"fmt"

	"github.com/niconiahi/the-agent/message"
	"github.com/niconiahi/the-agent/sender"
	"github.com/niconiahi/the-agent/tool"
)

func run_loop(invocation_context context.Context, agent_context *AgentContext, config *AgentLoopConfig, emit AgentEventSink) ([]message.Message, error) {
	emit(AgentStartEvent{})

	for {
		for {
			emit(TurnStartEvent{})

			assistant_message, error := stream_assistant_response(invocation_context, agent_context, config, emit)
			if error != nil {
				emit(TurnEndEvent{AssistantMessage: assistant_message})
				emit(AgentEndEvent{Messages: agent_context.Messages})
				return agent_context.Messages, error
			}

			if assistant_message.StopReason == message.STOP_REASON_ERROR || assistant_message.StopReason == message.STOP_REASON_ABORTED {
				emit(TurnEndEvent{AssistantMessage: assistant_message})
				emit(AgentEndEvent{Messages: agent_context.Messages})
				if assistant_message.ErrorMessage != "" {
					return agent_context.Messages, fmt.Errorf("%s", assistant_message.ErrorMessage)
				}
				return agent_context.Messages, fmt.Errorf("stream ended with stop reason: %s", assistant_message.StopReason)
			}

			tool_calls := extract_tool_calls(assistant_message)

			if len(tool_calls) == 0 {
				emit(TurnEndEvent{AssistantMessage: assistant_message})
				break
			}

			tool_results, steering_messages, error := execute_tool_calls(invocation_context, agent_context, assistant_message, config, emit)
			if error != nil {
				emit(TurnEndEvent{AssistantMessage: assistant_message, ToolResults: tool_results})
				emit(AgentEndEvent{Messages: agent_context.Messages})
				return agent_context.Messages, error
			}

			for _, result := range tool_results {
				agent_context.Messages = append(agent_context.Messages, result)
			}

			emit(TurnEndEvent{AssistantMessage: assistant_message, ToolResults: tool_results})

			if len(steering_messages) > 0 {
				for _, steering_message := range steering_messages {
					agent_context.Messages = append(agent_context.Messages, steering_message)
				}
				continue
			}

			if config.GetSteeringMessages != nil {
				steering := config.GetSteeringMessages()
				if len(steering) > 0 {
					for _, steering_message := range steering {
						agent_context.Messages = append(agent_context.Messages, steering_message)
					}
					continue
				}
			}
		}

		if config.GetFollowUpMessages != nil {
			follow_ups := config.GetFollowUpMessages()
			if len(follow_ups) > 0 {
				for _, follow_up := range follow_ups {
					agent_context.Messages = append(agent_context.Messages, follow_up)
				}
				continue
			}
		}

		break
	}

	emit(AgentEndEvent{Messages: agent_context.Messages})
	return agent_context.Messages, nil
}

func stream_assistant_response(invocation_context context.Context, agent_context *AgentContext, config *AgentLoopConfig, emit AgentEventSink) (*message.AssistantMessage, error) {
	messages := agent_context.Messages

	if config.TransformContext != nil {
		messages = config.TransformContext(invocation_context, messages)
	}

	if config.ConvertToLLM != nil {
		messages = config.ConvertToLLM(messages)
	}

	tool_schemas := make([]sender.ToolSchema, 0, len(agent_context.Tools))
	for _, t := range agent_context.Tools {
		tool_schemas = append(tool_schemas, tool.ToSchema(t))
	}

	llm_context := &sender.LLMContext{
		SystemPrompt: agent_context.SystemPrompt,
		Messages:     messages,
		Tools:        tool_schemas,
	}

	options := config.Options
	if options == nil {
		options = &sender.StreamOptions{}
	}

	if config.GetAPIKey != nil {
		api_key, error := config.GetAPIKey(config.Model.Provider)
		if error != nil {
			return nil, fmt.Errorf("failed to get API key: %v", error)
		}
		options.APIKey = api_key
	}

	event_stream, error := sender.Stream(invocation_context, config.Model, llm_context, options)
	if error != nil {
		return nil, error
	}

	var final_message *message.AssistantMessage

	for event := range event_stream.Events() {
		switch typed := event.(type) {
		case sender.EventStart:
			emit(MessageStartEvent{Message: typed.Message})
		case sender.EventDone:
			final_message = typed.Message
			emit(MessageEndEvent{Message: typed.Message})
		case sender.EventError:
			final_message = typed.Message
			emit(MessageEndEvent{Message: typed.Message})
		default:
			emit(MessageUpdateEvent{SenderEvent: event})
		}
	}

	if final_message == nil {
		final_message = event_stream.Result()
	}

	if final_message == nil {
		return nil, fmt.Errorf("stream ended without producing a message")
	}

	agent_context.Messages = append(agent_context.Messages, *final_message)
	return final_message, nil
}

func extract_tool_calls(assistant_message *message.AssistantMessage) []message.ToolCall {
	if assistant_message == nil {
		return nil
	}

	var tool_calls []message.ToolCall
	for _, content := range assistant_message.Content {
		if tc, ok := content.(message.ToolCall); ok {
			tool_calls = append(tool_calls, tc)
		}
	}
	return tool_calls
}
