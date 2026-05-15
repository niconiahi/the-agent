package sender

import (
	"context"
	"encoding/json"

	"github.com/niconiahi/the-agent/message"
	"github.com/niconiahi/the-agent/model"
)

type ToolSchema struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type LLMContext struct {
	SystemPrompt string            `json:"system_prompt"`
	Messages     []message.Message `json:"messages"`
	Tools        []ToolSchema      `json:"tools"`
}

type StreamFunction func(context.Context, *model.Model, *LLMContext, *StreamOptions) *EventStream

type Provider struct {
	API            string
	StreamFunction StreamFunction
}

var provider_registry = map[string]*Provider{}

func RegisterProvider(provider *Provider) {
	provider_registry[provider.API] = provider
}

func GetProvider(api string) *Provider {
	return provider_registry[api]
}
