package sender

import "github.com/niconiahi/the-agent/message"

type EventStream struct {
	events chan Event
	result chan *message.AssistantMessage
	done   chan struct{}
}

func NewEventStream() *EventStream {
	return &EventStream{
		events: make(chan Event, 64),
		result: make(chan *message.AssistantMessage, 1),
		done:   make(chan struct{}),
	}
}

func (stream *EventStream) Push(event Event) {
	stream.events <- event

	switch typed := event.(type) {
	case EventDone:
		stream.result <- typed.Message
	case EventError:
		stream.result <- typed.Message
	}
}

func (stream *EventStream) Close() {
	close(stream.events)
	close(stream.done)
}

func (stream *EventStream) Events() <-chan Event {
	return stream.events
}

func (stream *EventStream) Result() *message.AssistantMessage {
	return <-stream.result
}

func (stream *EventStream) Wait() {
	<-stream.done
}
