package orchestrator

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/niconiahi/the-agent/message"
	"github.com/niconiahi/the-agent/tool"
)

func execute_tool_calls(invocation_context context.Context, agent_context *AgentContext, assistant_message *message.AssistantMessage, config *AgentLoopConfig, emit AgentEventSink) ([]message.ToolResultMessage, []message.Message, error) {
	tool_calls := extract_tool_calls(assistant_message)
	if len(tool_calls) == 0 {
		return nil, nil, nil
	}

	if config.ToolExecution == TOOL_EXECUTION_PARALLEL {
		return execute_parallel(invocation_context, agent_context, assistant_message, tool_calls, config, emit)
	}

	return execute_sequential(invocation_context, agent_context, assistant_message, tool_calls, config, emit)
}

func execute_sequential(invocation_context context.Context, agent_context *AgentContext, assistant_message *message.AssistantMessage, tool_calls []message.ToolCall, config *AgentLoopConfig, emit AgentEventSink) ([]message.ToolResultMessage, []message.Message, error) {
	var results []message.ToolResultMessage
	var steering_messages []message.Message

	for _, tc := range tool_calls {
		if len(steering_messages) > 0 {
			results = append(results, skip_tool_call(tc))
			continue
		}

		emit(ToolExecutionStartEvent{
			ToolCallID: tc.ID,
			ToolName:   tc.Name,
			Arguments:  tc.Arguments,
		})

		target_tool := find_tool(agent_context.Tools, tc.Name)
		if target_tool == nil {
			result := error_tool_result(tc, fmt.Sprintf("unknown tool: %s", tc.Name))
			results = append(results, result)
			emit(ToolExecutionEndEvent{ToolCallID: tc.ID, ToolName: tc.Name, IsError: true})
			emit(MessageStartEvent{Message: result})
			emit(MessageEndEvent{Message: result})
			continue
		}

		if config.BeforeToolCall != nil {
			before_result := config.BeforeToolCall(invocation_context, BeforeToolCallContext{
				AssistantMessage: assistant_message,
				ToolCall:         tc,
				Arguments:        tc.Arguments,
				AgentContext:     agent_context,
			})
			if before_result != nil && before_result.Block {
				result := error_tool_result(tc, before_result.Reason)
				results = append(results, result)
				emit(ToolExecutionEndEvent{ToolCallID: tc.ID, ToolName: tc.Name, IsError: true})
				emit(MessageStartEvent{Message: result})
				emit(MessageEndEvent{Message: result})
				continue
			}
		}

		tool_result, error := target_tool.Execute(invocation_context, tc.ID, tc.Arguments)
		is_error := error != nil

		if error != nil {
			tool_result = tool.ToolResult{
				Content: []message.Content{message.TextContent{Text: error.Error()}},
			}
		}

		if config.AfterToolCall != nil {
			after_result := config.AfterToolCall(invocation_context, AfterToolCallContext{
				AssistantMessage: assistant_message,
				ToolCall:         tc,
				Arguments:        tc.Arguments,
				Result:           tool_result,
				IsError:          is_error,
				AgentContext:     agent_context,
			})
			if after_result != nil {
				if after_result.Content != nil {
					tool_result.Content = after_result.Content
				}
				if after_result.Details != nil {
					tool_result.Details = after_result.Details
				}
				if after_result.IsError != nil {
					is_error = *after_result.IsError
				}
			}
		}

		result := message.ToolResultMessage{
			ToolCallID: tc.ID,
			ToolName:   tc.Name,
			Content:    tool_result.Content,
			Details:    tool_result.Details,
			IsError:    is_error,
			Timestamp:  time.Now(),
		}

		results = append(results, result)
		emit(ToolExecutionEndEvent{ToolCallID: tc.ID, ToolName: tc.Name, Result: tool_result, IsError: is_error})
		emit(MessageStartEvent{Message: result})
		emit(MessageEndEvent{Message: result})

		if config.GetSteeringMessages != nil {
			steering := config.GetSteeringMessages()
			if len(steering) > 0 {
				steering_messages = append(steering_messages, steering...)
			}
		}
	}

	return results, steering_messages, nil
}

func execute_parallel(invocation_context context.Context, agent_context *AgentContext, assistant_message *message.AssistantMessage, tool_calls []message.ToolCall, config *AgentLoopConfig, emit AgentEventSink) ([]message.ToolResultMessage, []message.Message, error) {
	type parallel_result struct {
		tool_result tool.ToolResult
		error       error
		blocked     bool
		reason      string
	}

	parallel_results := make([]parallel_result, len(tool_calls))
	allowed := make([]bool, len(tool_calls))

	for i, tc := range tool_calls {
		emit(ToolExecutionStartEvent{
			ToolCallID: tc.ID,
			ToolName:   tc.Name,
			Arguments:  tc.Arguments,
		})

		target_tool := find_tool(agent_context.Tools, tc.Name)
		if target_tool == nil {
			parallel_results[i] = parallel_result{error: fmt.Errorf("unknown tool: %s", tc.Name)}
			continue
		}

		if config.BeforeToolCall != nil {
			before_result := config.BeforeToolCall(invocation_context, BeforeToolCallContext{
				AssistantMessage: assistant_message,
				ToolCall:         tc,
				Arguments:        tc.Arguments,
				AgentContext:     agent_context,
			})
			if before_result != nil && before_result.Block {
				parallel_results[i] = parallel_result{blocked: true, reason: before_result.Reason}
				continue
			}
		}

		allowed[i] = true
	}

	var wait_group sync.WaitGroup
	for i, tc := range tool_calls {
		if !allowed[i] {
			continue
		}

		target_tool := find_tool(agent_context.Tools, tc.Name)
		if target_tool == nil {
			continue
		}

		wait_group.Add(1)
		go func(index int, tool_call message.ToolCall, t *tool.Tool) {
			defer wait_group.Done()
			defer func() {
				if r := recover(); r != nil {
					parallel_results[index] = parallel_result{error: fmt.Errorf("tool %s panicked: %v", tool_call.Name, r)}
				}
			}()
			result, error := t.Execute(invocation_context, tool_call.ID, tool_call.Arguments)
			parallel_results[index] = parallel_result{tool_result: result, error: error}
		}(i, tc, target_tool)
	}
	wait_group.Wait()

	var results []message.ToolResultMessage
	for i, tc := range tool_calls {
		pr := parallel_results[i]

		if pr.blocked {
			result := error_tool_result(tc, pr.reason)
			results = append(results, result)
			emit(ToolExecutionEndEvent{ToolCallID: tc.ID, ToolName: tc.Name, IsError: true})
			emit(MessageStartEvent{Message: result})
			emit(MessageEndEvent{Message: result})
			continue
		}

		tool_result := pr.tool_result
		is_error := pr.error != nil

		if pr.error != nil {
			tool_result = tool.ToolResult{
				Content: []message.Content{message.TextContent{Text: pr.error.Error()}},
			}
		}

		if config.AfterToolCall != nil {
			after_result := config.AfterToolCall(invocation_context, AfterToolCallContext{
				AssistantMessage: assistant_message,
				ToolCall:         tc,
				Arguments:        tc.Arguments,
				Result:           tool_result,
				IsError:          is_error,
				AgentContext:     agent_context,
			})
			if after_result != nil {
				if after_result.Content != nil {
					tool_result.Content = after_result.Content
				}
				if after_result.Details != nil {
					tool_result.Details = after_result.Details
				}
				if after_result.IsError != nil {
					is_error = *after_result.IsError
				}
			}
		}

		result := message.ToolResultMessage{
			ToolCallID: tc.ID,
			ToolName:   tc.Name,
			Content:    tool_result.Content,
			Details:    tool_result.Details,
			IsError:    is_error,
			Timestamp:  time.Now(),
		}

		results = append(results, result)
		emit(ToolExecutionEndEvent{ToolCallID: tc.ID, ToolName: tc.Name, Result: tool_result, IsError: is_error})
		emit(MessageStartEvent{Message: result})
		emit(MessageEndEvent{Message: result})
	}

	var steering_messages []message.Message
	if config.GetSteeringMessages != nil {
		steering := config.GetSteeringMessages()
		if len(steering) > 0 {
			steering_messages = append(steering_messages, steering...)
		}
	}

	return results, steering_messages, nil
}

func find_tool(tools []tool.Tool, name string) *tool.Tool {
	for i := range tools {
		if tools[i].Name == name {
			return &tools[i]
		}
	}
	return nil
}

func skip_tool_call(tc message.ToolCall) message.ToolResultMessage {
	return message.ToolResultMessage{
		ToolCallID: tc.ID,
		ToolName:   tc.Name,
		Content:    []message.Content{message.TextContent{Text: "Skipped due to queued user message"}},
		IsError:    true,
		Timestamp:  time.Now(),
	}
}

func error_tool_result(tc message.ToolCall, reason string) message.ToolResultMessage {
	return message.ToolResultMessage{
		ToolCallID: tc.ID,
		ToolName:   tc.Name,
		Content:    []message.Content{message.TextContent{Text: reason}},
		IsError:    true,
		Timestamp:  time.Now(),
	}
}
