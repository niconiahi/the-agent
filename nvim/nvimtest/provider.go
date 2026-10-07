package nvimtest

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/niconiahi/the-agent/message"
	"github.com/niconiahi/the-agent/model"
	"github.com/niconiahi/the-agent/nvim"
	"github.com/niconiahi/the-agent/sender"
)

// FAKE_API is the provider-registry key of the fake provider and Model.
const FAKE_API = "nvimtest-fake"

// Model is a model served by the fake provider.
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

// Config is an nvim.Config wired to the fake Model, with no tools.
func Config() nvim.Config {
	return nvim.Config{
		Model:        Model(),
		SystemPrompt: "You are a test agent.",
	}
}

// Reply is one scripted assistant response. Its text is streamed as one
// EventTextDelta per entry in Deltas, sleeping Delay before each one.
type Reply struct {
	Deltas      []string
	Delay       time.Duration
	TotalTokens int
	// StopReason defaults to stop. Use error to simulate a failed request.
	StopReason   message.StopReason
	ErrorMessage string
}

// Text is a reply that streams text as a single delta.
func Text(text string, total_tokens int) Reply {
	return Reply{Deltas: []string{text}, TotalTokens: total_tokens}
}

// Provider is the registered fake. It records every request it receives.
type Provider struct {
	mutex    sync.Mutex
	replies  []Reply
	requests []sender.LLMContext
}

// RegisterProvider registers a fake provider under FAKE_API that answers
// requests with replies, in order. The provider registry is a global map, so
// tests using it must not run in parallel.
func RegisterProvider(t *testing.T, replies ...Reply) *Provider {
	provider := &Provider{replies: replies}
	sender.RegisterProvider(&sender.Provider{API: FAKE_API, StreamFunction: provider.stream})
	t.Cleanup(func() {
		sender.RegisterProvider(&sender.Provider{API: FAKE_API})
	})
	return provider
}

// Requests returns copies of the contexts the provider was called with.
func (provider *Provider) Requests() []sender.LLMContext {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return append([]sender.LLMContext(nil), provider.requests...)
}

func (provider *Provider) stream(ctx context.Context, target *model.Model, llm_context *sender.LLMContext, _ *sender.StreamOptions) *sender.EventStream {
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

		text := ""
		if len(reply.Deltas) > 0 {
			stream.Push(sender.EventTextStart{ContentIndex: 0, Message: output})
		}
		for _, delta := range reply.Deltas {
			select {
			case <-ctx.Done():
				aborted := &message.AssistantMessage{StopReason: message.STOP_REASON_ABORTED, Timestamp: time.Now()}
				stream.Push(sender.EventError{StopReason: message.STOP_REASON_ABORTED, Message: aborted})
				return
			case <-time.After(reply.Delay):
			}
			text += delta
			stream.Push(sender.EventTextDelta{ContentIndex: 0, Delta: delta, Message: output})
		}
		if len(reply.Deltas) > 0 {
			stream.Push(sender.EventTextEnd{ContentIndex: 0, FullText: text, Message: output})
		}

		final := *output
		if text != "" {
			final.Content = []message.Content{message.TextContent{Text: text}}
		}
		final.Usage = message.Usage{TotalTokens: reply.TotalTokens}
		final.StopReason = reply.StopReason
		if final.StopReason == "" {
			final.StopReason = message.STOP_REASON_STOP
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
