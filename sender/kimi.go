package sender

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/niconiahi/the-agent/message"
	"github.com/niconiahi/the-agent/model"
)

func init() {
	RegisterProvider(&Provider{
		API:            "openai-completions",
		StreamFunction: kimi_stream,
	})
}

type openai_message struct {
	Role             string          `json:"role"`
	Content          interface{}     `json:"content,omitempty"`
	ToolCalls        []openai_tool_call `json:"tool_calls,omitempty"`
	ToolCallID       string          `json:"tool_call_id,omitempty"`
	ReasoningContent string          `json:"reasoning_content,omitempty"`
}

type openai_tool_call struct {
	ID       string              `json:"id"`
	Type     string              `json:"type"`
	Function openai_function_call `json:"function"`
}

type openai_function_call struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openai_tool struct {
	Type     string          `json:"type"`
	Function openai_function `json:"function"`
}

type openai_function struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type openai_content_part struct {
	Type     string          `json:"type"`
	Text     string          `json:"text,omitempty"`
	ImageURL *openai_image_url `json:"image_url,omitempty"`
}

type openai_image_url struct {
	URL string `json:"url"`
}

type openai_request struct {
	Model       string           `json:"model"`
	Messages    []openai_message `json:"messages"`
	Tools       []openai_tool    `json:"tools,omitempty"`
	Stream      bool             `json:"stream"`
	Temperature *float64         `json:"temperature,omitempty"`
	MaxTokens   *int             `json:"max_tokens,omitempty"`
}

type openai_chunk struct {
	Choices []openai_choice `json:"choices"`
	Usage   *openai_usage   `json:"usage,omitempty"`
}

type openai_choice struct {
	Delta        openai_delta `json:"delta"`
	FinishReason *string      `json:"finish_reason"`
}

type openai_delta struct {
	Content          *string              `json:"content"`
	ReasoningContent *string              `json:"reasoning_content"`
	ToolCalls        []openai_delta_tool_call `json:"tool_calls"`
}

type openai_delta_tool_call struct {
	Index    int                 `json:"index"`
	ID       string              `json:"id,omitempty"`
	Type     string              `json:"type,omitempty"`
	Function openai_delta_function `json:"function"`
}

type openai_delta_function struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type openai_usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

const (
	BLOCK_NONE     = "none"
	BLOCK_TEXT     = "text"
	BLOCK_THINKING = "thinking"
	BLOCK_TOOL     = "tool"
)

func convert_message(input message.Message) openai_message {
	switch typed := input.(type) {
	case message.UserMessage:
		return convert_user_message(typed)
	case message.AssistantMessage:
		return convert_assistant_message(typed)
	case message.ToolResultMessage:
		return convert_tool_result_message(typed)
	default:
		return openai_message{}
	}
}

func convert_user_message(input message.UserMessage) openai_message {
	has_images := false
	for _, content := range input.Content {
		if _, ok := content.(message.ImageContent); ok {
			has_images = true
			break
		}
	}

	if !has_images {
		text := ""
		for _, content := range input.Content {
			if tc, ok := content.(message.TextContent); ok {
				text += tc.Text
			}
		}
		return openai_message{Role: "user", Content: text}
	}

	parts := []openai_content_part{}
	for _, content := range input.Content {
		switch typed := content.(type) {
		case message.TextContent:
			parts = append(parts, openai_content_part{Type: "text", Text: typed.Text})
		case message.ImageContent:
			url := fmt.Sprintf("data:%s;base64,%s", typed.MimeType, typed.Data)
			parts = append(parts, openai_content_part{
				Type:     "image_url",
				ImageURL: &openai_image_url{URL: url},
			})
		}
	}
	return openai_message{Role: "user", Content: parts}
}

func convert_assistant_message(input message.AssistantMessage) openai_message {
	text := ""
	reasoning := ""
	var tool_calls []openai_tool_call

	for _, content := range input.Content {
		switch typed := content.(type) {
		case message.TextContent:
			text += typed.Text
		case message.ThinkingContent:
			reasoning += typed.Thinking
		case message.ToolCall:
			args_bytes, marshal_err := json.Marshal(typed.Arguments)
			if marshal_err != nil {
				args_bytes = []byte(fmt.Sprintf(`{"_marshal_error":"%v"}`, marshal_err))
			}
			tool_calls = append(tool_calls, openai_tool_call{
				ID:   typed.ID,
				Type: "function",
				Function: openai_function_call{
					Name:      typed.Name,
					Arguments: string(args_bytes),
				},
			})
		}
	}

	output := openai_message{Role: "assistant"}
	if text != "" {
		output.Content = text
	}
	if reasoning != "" {
		output.ReasoningContent = reasoning
	}
	if len(tool_calls) > 0 {
		output.ToolCalls = tool_calls
	}
	return output
}

func convert_tool_result_message(input message.ToolResultMessage) openai_message {
	text := ""
	for _, content := range input.Content {
		if tc, ok := content.(message.TextContent); ok {
			text += tc.Text
		}
	}
	return openai_message{
		Role:       "tool",
		Content:    text,
		ToolCallID: input.ToolCallID,
	}
}

func convert_tool_schema(schema ToolSchema) openai_tool {
	return openai_tool{
		Type: "function",
		Function: openai_function{
			Name:        schema.Name,
			Description: schema.Description,
			Parameters:  schema.Parameters,
		},
	}
}

func parse_sse(reader io.Reader, data_channel chan<- string, sse_error chan<- error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()

		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")

		if data == "[DONE]" {
			close(data_channel)
			return
		}

		data_channel <- data
	}

	if err := scanner.Err(); err != nil {
		sse_error <- err
	}
	close(data_channel)
}

func kimi_stream(invocation_context context.Context, target *model.Model, llm_context *LLMContext, options *StreamOptions) *EventStream {
	stream := NewEventStream()

	go func() {
		defer stream.Close()

		sse_error := make(chan error, 1)

		messages := []openai_message{}
		if llm_context.SystemPrompt != "" {
			messages = append(messages, openai_message{Role: "system", Content: llm_context.SystemPrompt})
		}
		for _, input := range llm_context.Messages {
			messages = append(messages, convert_message(input))
		}

		tools := []openai_tool{}
		for _, schema := range llm_context.Tools {
			tools = append(tools, convert_tool_schema(schema))
		}

		request_body := openai_request{
			Model:       target.ID,
			Messages:    messages,
			Stream:      true,
			Temperature: options.Temperature,
			MaxTokens:   options.MaxTokens,
		}
		if len(tools) > 0 {
			request_body.Tools = tools
		}

		body_bytes, error := json.Marshal(request_body)
		if error != nil {
			emit_error(stream, target, fmt.Sprintf("failed to marshal request: %v", error))
			return
		}

		request, error := http.NewRequestWithContext(
			invocation_context,
			http.MethodPost,
			target.BaseURL+"/chat/completions",
			bytes.NewReader(body_bytes),
		)
		if error != nil {
			emit_error(stream, target, fmt.Sprintf("failed to create request: %v", error))
			return
		}

		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+options.APIKey)
		for key, value := range options.Headers {
			request.Header.Set(key, value)
		}

		response, error := http.DefaultClient.Do(request)
		if error != nil {
			if invocation_context.Err() != nil {
				emit_aborted(stream, target)
				return
			}
			emit_error(stream, target, fmt.Sprintf("request failed: %v", error))
			return
		}
		defer response.Body.Close()

		if response.StatusCode != http.StatusOK {
			error_body, read_err := io.ReadAll(response.Body)
			if read_err != nil {
				emit_error(stream, target, fmt.Sprintf("API error %d (failed to read response body: %v)", response.StatusCode, read_err))
			} else {
				emit_error(stream, target, fmt.Sprintf("API error %d: %s", response.StatusCode, string(error_body)))
			}
			return
		}

		assistant_message := &message.AssistantMessage{
			API:       target.API,
			Provider:  target.Provider,
			Model:     target.ID,
			Timestamp: time.Now(),
		}

		stream.Push(EventStart{Message: assistant_message})

		data_channel := make(chan string, 64)
		go parse_sse(response.Body, data_channel, sse_error)

		current_block := BLOCK_NONE
		content_index := 0
		tool_call_args := map[int]string{}
		tool_call_ids := map[int]string{}
		tool_call_names := map[int]string{}

		for {
			select {
			case <-invocation_context.Done():
				finalize_block(stream, assistant_message, current_block, content_index, tool_call_ids, tool_call_names, tool_call_args)
				emit_aborted(stream, target)
				return

			case sse_err := <-sse_error:
				finalize_block(stream, assistant_message, current_block, content_index, tool_call_ids, tool_call_names, tool_call_args)
				emit_error(stream, target, fmt.Sprintf("SSE stream read error: %v", sse_err))
				return

			case data, ok := <-data_channel:
				if !ok {
					finalize_block(stream, assistant_message, current_block, content_index, tool_call_ids, tool_call_names, tool_call_args)
					select {
					case sse_err := <-sse_error:
						emit_error(stream, target, fmt.Sprintf("SSE stream read error: %v", sse_err))
						return
					default:
					}
					if assistant_message.StopReason == "" {
						assistant_message.StopReason = message.STOP_REASON_STOP
					}
					stream.Push(EventDone{
						StopReason: assistant_message.StopReason,
						Message:    assistant_message,
					})
					return
				}

				var chunk openai_chunk
				if json.Unmarshal([]byte(data), &chunk) != nil {
					continue
				}

				if chunk.Usage != nil {
					assistant_message.Usage = message.Usage{
						InputTokens:  chunk.Usage.PromptTokens,
						OutputTokens: chunk.Usage.CompletionTokens,
						TotalTokens:  chunk.Usage.TotalTokens,
					}
				}

				if len(chunk.Choices) == 0 {
					continue
				}

				choice := chunk.Choices[0]

				if choice.FinishReason != nil {
					switch *choice.FinishReason {
					case "stop":
						assistant_message.StopReason = message.STOP_REASON_STOP
					case "length":
						assistant_message.StopReason = message.STOP_REASON_LENGTH
					case "tool_calls":
						assistant_message.StopReason = message.STOP_REASON_TOOL_USE
					}
				}

				if choice.Delta.ReasoningContent != nil && *choice.Delta.ReasoningContent != "" {
					if current_block != BLOCK_THINKING {
						finalize_block(stream, assistant_message, current_block, content_index, tool_call_ids, tool_call_names, tool_call_args)
						if current_block != BLOCK_NONE {
							content_index++
						}
						current_block = BLOCK_THINKING
						assistant_message.Content = append(assistant_message.Content, message.ThinkingContent{})
						stream.Push(EventThinkingStart{ContentIndex: content_index, Message: assistant_message})
					}
					delta := *choice.Delta.ReasoningContent
					update_thinking_content(assistant_message, content_index, delta)
					stream.Push(EventThinkingDelta{ContentIndex: content_index, Delta: delta, Message: assistant_message})
				}

				if choice.Delta.Content != nil && *choice.Delta.Content != "" {
					if current_block != BLOCK_TEXT {
						finalize_block(stream, assistant_message, current_block, content_index, tool_call_ids, tool_call_names, tool_call_args)
						if current_block != BLOCK_NONE {
							content_index++
						}
						current_block = BLOCK_TEXT
						assistant_message.Content = append(assistant_message.Content, message.TextContent{})
						stream.Push(EventTextStart{ContentIndex: content_index, Message: assistant_message})
					}
					delta := *choice.Delta.Content
					update_text_content(assistant_message, content_index, delta)
					stream.Push(EventTextDelta{ContentIndex: content_index, Delta: delta, Message: assistant_message})
				}

				for _, tc := range choice.Delta.ToolCalls {
					tool_index := tc.Index
					if tc.ID != "" {
						finalize_block(stream, assistant_message, current_block, content_index, tool_call_ids, tool_call_names, tool_call_args)
						if current_block != BLOCK_NONE {
							content_index++
						}
						current_block = BLOCK_TOOL
						tool_call_ids[tool_index] = tc.ID
						tool_call_names[tool_index] = tc.Function.Name
						tool_call_args[tool_index] = ""
						assistant_message.Content = append(assistant_message.Content, message.ToolCall{})
						stream.Push(EventToolCallStart{ContentIndex: content_index, ID: tc.ID, Name: tc.Function.Name, Message: assistant_message})
					}
					if tc.Function.Arguments != "" {
						tool_call_args[tool_index] += tc.Function.Arguments
						stream.Push(EventToolCallDelta{ContentIndex: content_index, Delta: tc.Function.Arguments, Message: assistant_message})
					}
				}
			}
		}
	}()

	return stream
}

func finalize_block(stream *EventStream, assistant_message *message.AssistantMessage, block_type string, content_index int, tool_call_ids, tool_call_names map[int]string, tool_call_args map[int]string) {
	switch block_type {
	case BLOCK_TEXT:
		if content_index < len(assistant_message.Content) {
			if tc, ok := assistant_message.Content[content_index].(message.TextContent); ok {
				stream.Push(EventTextEnd{ContentIndex: content_index, FullText: tc.Text, Message: assistant_message})
			}
		}
	case BLOCK_THINKING:
		if content_index < len(assistant_message.Content) {
			if tc, ok := assistant_message.Content[content_index].(message.ThinkingContent); ok {
				stream.Push(EventThinkingEnd{ContentIndex: content_index, FullText: tc.Thinking, Message: assistant_message})
			}
		}
	case BLOCK_TOOL:
		for tool_index, id := range tool_call_ids {
			var arguments map[string]interface{}
			if args_str, ok := tool_call_args[tool_index]; ok && args_str != "" {
				if err := json.Unmarshal([]byte(args_str), &arguments); err != nil {
					arguments = map[string]interface{}{"_parse_error": fmt.Sprintf("failed to parse tool arguments: %v", err), "_raw": args_str}
				}
			}
			tool_call := message.ToolCall{
				ID:        id,
				Name:      tool_call_names[tool_index],
				Arguments: arguments,
			}
			if content_index < len(assistant_message.Content) {
				assistant_message.Content[content_index] = tool_call
			}
			stream.Push(EventToolCallEnd{ContentIndex: content_index, ToolCall: tool_call, Message: assistant_message})
		}
		for key := range tool_call_ids {
			delete(tool_call_ids, key)
		}
		for key := range tool_call_names {
			delete(tool_call_names, key)
		}
		for key := range tool_call_args {
			delete(tool_call_args, key)
		}
	}
}

func update_text_content(assistant_message *message.AssistantMessage, index int, delta string) {
	if index < len(assistant_message.Content) {
		if tc, ok := assistant_message.Content[index].(message.TextContent); ok {
			tc.Text += delta
			assistant_message.Content[index] = tc
		}
	}
}

func update_thinking_content(assistant_message *message.AssistantMessage, index int, delta string) {
	if index < len(assistant_message.Content) {
		if tc, ok := assistant_message.Content[index].(message.ThinkingContent); ok {
			tc.Thinking += delta
			assistant_message.Content[index] = tc
		}
	}
}

func emit_error(stream *EventStream, target *model.Model, error_message string) {
	stream.Push(EventError{
		StopReason: message.STOP_REASON_ERROR,
		Message: &message.AssistantMessage{
			API:          target.API,
			Provider:     target.Provider,
			Model:        target.ID,
			StopReason:   message.STOP_REASON_ERROR,
			ErrorMessage: error_message,
			Timestamp:    time.Now(),
		},
	})
}

func emit_aborted(stream *EventStream, target *model.Model) {
	stream.Push(EventError{
		StopReason: message.STOP_REASON_ABORTED,
		Message: &message.AssistantMessage{
			API:        target.API,
			Provider:   target.Provider,
			Model:      target.ID,
			StopReason: message.STOP_REASON_ABORTED,
			Timestamp:  time.Now(),
		},
	})
}
