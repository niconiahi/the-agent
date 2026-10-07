package nvimtest

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/niconiahi/the-agent/message"
	"github.com/niconiahi/the-agent/model"
	"github.com/niconiahi/the-agent/nvim"
	"github.com/niconiahi/the-agent/sender"
)

const FAKE_API = "nvimtest-fake"

func Model() *model.Model {
	return &model.Model{
		ID:            "fake-model",
		Name:          "Fake Model",
		API:           FAKE_API,
		Provider:      "nvimtest",
		ContextWindow: 262144,
		MaxTokens:     32768,
	}
}

func Config() nvim.Config {
	return nvim.Config{
		Model:        Model(),
		SystemPrompt: "You are a test agent.",
	}
}

type Reply struct {
	Thinking    []string
	Deltas      []string
	ToolCalls   []message.ToolCall
	Delay       time.Duration
	TotalTokens int

	StopReason   message.StopReason
	ErrorMessage string

	// Fragments, when set, holds for each tool call the argument JSON to
	// stream as separate deltas, in place of its marshalled Arguments. The
	// ToolCallEnd still carries the call as given.
	Fragments [][]string
	// Gate, when set, holds the reply's tool calls before each argument
	// fragment and before each ToolCallEnd until the test steps it.
	Gate *Gate
}

// Gate pauses a scripted stream between tool-argument fragments so a test
// can look at Neovim at each point of the stream.
type Gate struct {
	steps chan struct{}
}

func NewGate() *Gate {
	return &Gate{steps: make(chan struct{})}
}

// Step lets the next fragment (or the ToolCallEnd after the last one)
// through, returning once the stream has taken it.
func (gate *Gate) Step(t *testing.T) {
	t.Helper()
	select {
	case gate.steps <- struct{}{}:
	case <-time.After(5 * time.Second):
		t.Fatal("nvimtest: the stream never reached the gate")
	}
}

func Text(text string, total_tokens int) Reply {
	return Reply{Deltas: []string{text}, TotalTokens: total_tokens}
}

type Provider struct {
	mutex    sync.Mutex
	replies  []Reply
	requests []sender.LLMContext
}

func RegisterProvider(t *testing.T, replies ...Reply) *Provider {
	provider := &Provider{replies: replies}
	sender.RegisterProvider(&sender.Provider{API: FAKE_API, StreamFunction: provider.stream})
	t.Cleanup(func() {
		sender.RegisterProvider(&sender.Provider{API: FAKE_API})
	})
	return provider
}

func (provider *Provider) Requests() []sender.LLMContext {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return append([]sender.LLMContext(nil), provider.requests...)
}

func (provider *Provider) stream(invocation_context context.Context, target *model.Model, llm_context *sender.LLMContext, _ *sender.StreamOptions) *sender.EventStream {
	provider.mutex.Lock()
	provider.requests = append(provider.requests, sender.LLMContext{
		SystemPrompt: llm_context.SystemPrompt,
		Messages:     append([]message.Message(nil), llm_context.Messages...),
		Tools:        llm_context.Tools,
	})
	reply := Reply{StopReason: message.STOP_REASON_ERROR, ErrorMessage: "nvimtest: no scripted reply left"}
	if len(provider.replies) > 0 {
		reply = provider.replies[0]
		provider.replies = provider.replies[1:]
	}
	provider.mutex.Unlock()

	stream := sender.NewEventStream()
	go func() {
		defer stream.Close()
		output := &message.AssistantMessage{
			API:       target.API,
			Provider:  target.Provider,
			Model:     target.ID,
			Timestamp: time.Now(),
		}
		stream.Push(sender.EventStart{Message: output})

		wait := func() bool {
			select {
			case <-invocation_context.Done():
				aborted := &message.AssistantMessage{StopReason: message.STOP_REASON_ABORTED, Timestamp: time.Now()}
				stream.Push(sender.EventError{StopReason: message.STOP_REASON_ABORTED, Message: aborted})
				return false
			case <-time.After(reply.Delay):
				return true
			}
		}

		content := []message.Content{}
		if len(reply.Thinking) > 0 {
			index := len(content)
			stream.Push(sender.EventThinkingStart{ContentIndex: index, Message: output})
			thinking := ""
			for _, delta := range reply.Thinking {
				if !wait() {
					return
				}
				thinking += delta
				stream.Push(sender.EventThinkingDelta{ContentIndex: index, Delta: delta, Message: output})
			}
			stream.Push(sender.EventThinkingEnd{ContentIndex: index, FullText: thinking, Message: output})
			content = append(content, message.ThinkingContent{Thinking: thinking})
		}
		if len(reply.Deltas) > 0 {
			index := len(content)
			stream.Push(sender.EventTextStart{ContentIndex: index, Message: output})
			text := ""
			for _, delta := range reply.Deltas {
				if !wait() {
					return
				}
				text += delta
				stream.Push(sender.EventTextDelta{ContentIndex: index, Delta: delta, Message: output})
			}
			stream.Push(sender.EventTextEnd{ContentIndex: index, FullText: text, Message: output})
			if text != "" {
				content = append(content, message.TextContent{Text: text})
			}
		}
		hold := func() bool {
			if reply.Gate == nil {
				return true
			}
			select {
			case <-invocation_context.Done():
				aborted := &message.AssistantMessage{StopReason: message.STOP_REASON_ABORTED, Timestamp: time.Now()}
				stream.Push(sender.EventError{StopReason: message.STOP_REASON_ABORTED, Message: aborted})
				return false
			case <-reply.Gate.steps:
				return true
			}
		}
		for position, call := range reply.ToolCalls {
			index := len(content)
			stream.Push(sender.EventToolCallStart{ContentIndex: index, ID: call.ID, Name: call.Name, Message: output})
			if !wait() {
				return
			}
			arguments, _ := json.Marshal(call.Arguments)
			fragments := []string{string(arguments)}
			if position < len(reply.Fragments) {
				fragments = reply.Fragments[position]
			}
			for _, fragment := range fragments {
				if !hold() {
					return
				}
				stream.Push(sender.EventToolCallDelta{ContentIndex: index, Delta: fragment, Message: output})
			}
			if !hold() {
				return
			}
			stream.Push(sender.EventToolCallEnd{ContentIndex: index, ToolCall: call, Message: output})
			content = append(content, call)
		}

		final := *output
		if len(content) > 0 {
			final.Content = content
		}
		final.Usage = message.Usage{TotalTokens: reply.TotalTokens}
		final.StopReason = reply.StopReason
		if final.StopReason == "" {
			final.StopReason = message.STOP_REASON_STOP
			if len(reply.ToolCalls) > 0 {
				final.StopReason = message.STOP_REASON_TOOL_USE
			}
		}
		final.ErrorMessage = reply.ErrorMessage
		if final.StopReason == message.STOP_REASON_ERROR || final.StopReason == message.STOP_REASON_ABORTED {
			stream.Push(sender.EventError{StopReason: final.StopReason, Message: &final})
			return
		}
		stream.Push(sender.EventDone{StopReason: final.StopReason, Message: &final})
	}()
	return stream
}
