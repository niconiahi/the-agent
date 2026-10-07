package sender

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestKimiStream_ToolCallStartNamesTheCallBeforeItsArgumentsStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		chunks := []string{
			`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"tc_1","type":"function","function":{"name":"edit","arguments":""}}]}}]}`,
			`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"path\":"}}]}}]}`,
			`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"a.go\"}"}}]},"finish_reason":"tool_calls"}]}`,
		}
		for _, chunk := range chunks {
			fmt.Fprintf(writer, "data: %s\n\n", chunk)
		}
		fmt.Fprint(writer, "data: [DONE]\n\n")
	}))
	defer server.Close()
	target := kimi_model()
	target.BaseURL = server.URL

	stream := kimi_stream(context.Background(), target, &LLMContext{}, &StreamOptions{APIKey: "key"})
	var start *EventToolCallStart
	deltas := 0
	for event := range stream.Events() {
		switch typed := event.(type) {
		case EventToolCallStart:
			start = &typed
		case EventToolCallDelta:
			if start == nil {
				t.Fatal("a delta came before the start")
			}
			deltas++
		}
	}
	if start == nil || start.ID != "tc_1" || start.Name != "edit" {
		t.Fatalf("start: %#v", start)
	}
	if deltas != 2 {
		t.Fatalf("deltas: %d", deltas)
	}
}
