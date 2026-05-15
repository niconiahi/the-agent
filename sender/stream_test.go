package sender

import (
	"testing"
	"time"

	"github.com/niconiahi/the-agent/message"
)

func TestEventStream_PushAndReceive(t *testing.T) {
	stream := NewEventStream()
	expected := EventTextDelta{ContentIndex: 0, Delta: "hello"}

	go func() {
		stream.Push(expected)
		stream.Push(EventDone{Message: &message.AssistantMessage{}})
		stream.Close()
	}()

	event := <-stream.Events()
	delta, ok := event.(EventTextDelta)
	if !ok {
		t.Fatalf("expected EventTextDelta, got %T", event)
	}
	if delta.Delta != "hello" {
		t.Fatalf("expected delta 'hello', got '%s'", delta.Delta)
	}
}

func TestEventStream_Ordering(t *testing.T) {
	stream := NewEventStream()
	deltas := []string{"a", "b", "c", "d", "e"}

	go func() {
		for _, delta := range deltas {
			stream.Push(EventTextDelta{Delta: delta})
		}
		stream.Push(EventDone{Message: &message.AssistantMessage{}})
		stream.Close()
	}()

	var received []string
	for event := range stream.Events() {
		if delta, ok := event.(EventTextDelta); ok {
			received = append(received, delta.Delta)
		}
	}

	if len(received) != len(deltas) {
		t.Fatalf("expected %d deltas, got %d", len(deltas), len(received))
	}
	for i, delta := range received {
		if delta != deltas[i] {
			t.Errorf("position %d: expected '%s', got '%s'", i, deltas[i], delta)
		}
	}
}

func TestEventStream_CloseTerminatesRange(t *testing.T) {
	stream := NewEventStream()

	go func() {
		stream.Push(EventTextDelta{Delta: "one"})
		stream.Push(EventTextDelta{Delta: "two"})
		stream.Push(EventDone{Message: &message.AssistantMessage{}})
		stream.Close()
	}()

	count := 0
	for range stream.Events() {
		count++
	}

	if count != 3 {
		t.Fatalf("expected 3 events (2 deltas + done), got %d", count)
	}
}

func TestEventStream_ResultFromDone(t *testing.T) {
	stream := NewEventStream()
	expected := &message.AssistantMessage{Model: "test-model"}

	go func() {
		stream.Push(EventDone{Message: expected})
		stream.Close()
	}()

	for range stream.Events() {
	}

	result := stream.Result()
	if result.Model != "test-model" {
		t.Fatalf("expected model 'test-model', got '%s'", result.Model)
	}
}

func TestEventStream_ResultFromError(t *testing.T) {
	stream := NewEventStream()
	expected := &message.AssistantMessage{ErrorMessage: "something broke"}

	go func() {
		stream.Push(EventError{Message: expected})
		stream.Close()
	}()

	for range stream.Events() {
	}

	result := stream.Result()
	if result.ErrorMessage != "something broke" {
		t.Fatalf("expected error message 'something broke', got '%s'", result.ErrorMessage)
	}
}

func TestEventStream_WaitBlocksUntilClose(t *testing.T) {
	stream := NewEventStream()
	done := make(chan struct{})

	go func() {
		stream.Wait()
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("Wait() returned before Close()")
	case <-time.After(50 * time.Millisecond):
	}

	stream.Push(EventDone{Message: &message.AssistantMessage{}})
	stream.Close()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Wait() did not return after Close()")
	}
}

func TestEventStream_BufferCapacity(t *testing.T) {
	stream := NewEventStream()

	for i := 0; i < 63; i++ {
		stream.Push(EventTextDelta{Delta: "x"})
	}

	stream.Push(EventDone{Message: &message.AssistantMessage{}})
	stream.Close()

	count := 0
	for range stream.Events() {
		count++
	}

	if count != 64 {
		t.Fatalf("expected 64 events (63 deltas + done), got %d", count)
	}
}
