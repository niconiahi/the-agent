package sender

import (
	"context"
	"fmt"

	"github.com/niconiahi/the-agent/message"
	"github.com/niconiahi/the-agent/model"
)

func Stream(invocation_context context.Context, target *model.Model, llm_context *LLMContext, options *StreamOptions) (*EventStream, error) {
	provider := GetProvider(target.API)
	if provider == nil {
		return nil, fmt.Errorf("no provider registered for API type: %s", target.API)
	}
	return provider.StreamFunction(invocation_context, target, llm_context, options), nil
}

func Complete(invocation_context context.Context, target *model.Model, llm_context *LLMContext, options *StreamOptions) (*message.AssistantMessage, error) {
	stream, error := Stream(invocation_context, target, llm_context, options)
	if error != nil {
		return nil, error
	}

	for range stream.Events() {
	}

	return stream.Result(), nil
}
