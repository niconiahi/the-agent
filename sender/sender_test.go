package sender

import (
	"context"
	"testing"

	"github.com/niconiahi/the-agent/message"
	"github.com/niconiahi/the-agent/model"
)

func TestStream_UnknownProvider(t *testing.T) {
	target := &model.Model{API: "nonexistent-api"}
	_, error := Stream(context.Background(), target, &LLMContext{}, &StreamOptions{})

	if error == nil {
		t.Fatal("expected error for unknown provider")
	}
}

func TestStream_DelegatesToProvider(t *testing.T) {
	called := false
	RegisterProvider(&Provider{
		API: "test-delegate",
		StreamFunction: func(_ context.Context, _ *model.Model, _ *LLMContext, _ *StreamOptions) *EventStream {
			called = true
			stream := NewEventStream()
			go func() {
				stream.Push(EventDone{Message: &message.AssistantMessage{}})
				stream.Close()
			}()
			return stream
		},
	})
	defer delete(provider_registry, "test-delegate")

	target := &model.Model{API: "test-delegate"}
	stream, error := Stream(context.Background(), target, &LLMContext{}, &StreamOptions{})

	if error != nil {
		t.Fatalf("unexpected error: %v", error)
	}
	for range stream.Events() {
	}
	if !called {
		t.Fatal("provider stream function was not called")
	}
}

func TestComplete_DrainsAndReturnsResult(t *testing.T) {
	RegisterProvider(&Provider{
		API: "test-complete",
		StreamFunction: func(_ context.Context, _ *model.Model, _ *LLMContext, _ *StreamOptions) *EventStream {
			stream := NewEventStream()
			go func() {
				stream.Push(EventTextDelta{Delta: "hello"})
				stream.Push(EventDone{Message: &message.AssistantMessage{Model: "test"}})
				stream.Close()
			}()
			return stream
		},
	})
	defer delete(provider_registry, "test-complete")

	target := &model.Model{API: "test-complete"}
	result, error := Complete(context.Background(), target, &LLMContext{}, &StreamOptions{})

	if error != nil {
		t.Fatalf("unexpected error: %v", error)
	}
	if result == nil {
		t.Fatal("expected result, got nil")
	}
	if result.Model != "test" {
		t.Fatalf("expected model 'test', got '%s'", result.Model)
	}
}
