# Testing Strategy

## Philosophy

Build components sequentially. Test each in isolation. Finish with integration tests.

## Test Structure

```
internal/                              <- Unit tests (per package)
├── ai/
│   ├── provider.go
│   ├── provider_test.go
│   ├── stream.go
│   └── stream_test.go
├── agent/
│   ├── loop.go
│   ├── loop_test.go
│   ├── tool.go
│   └── tool_test.go
└── ...

integration/                           <- Integration tests (task-oriented)
├── stream_completion_test.go
├── agent_tool_execution_test.go
└── ...
```

## Unit Test Guidelines

### Isolation

Each component tests independently. No external dependencies in unit tests.

```go
// Good: Test stream parsing in isolation
func TestParseStreamEvent_TextDelta(test *testing.T) {
    input := `{"type":"text_delta","text":"hello"}`
    event, error := parse_stream_event([]byte(input))
    if error != nil {
        test.Fatal(error)
    }
    if event.Type != "text_delta" {
        test.Errorf("expected text_delta, got %s", event.Type)
    }
}

// Bad: Unit test that requires a running LLM API
func TestStream_WithRealAPI(test *testing.T) {
    // Don't do this - API calls are integration test concerns
}
```

### HTTP Client Tests

Mock HTTP for API clients:

```go
func TestKimiClient_Complete(test *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        json.NewEncoder(writer).Encode(map[string]any{
            "choices": []map[string]any{
                {"message": map[string]string{"content": "hello"}},
            },
        })
    }))
    defer server.Close()

    client := NewClient(server.URL)
    response, error := client.Complete(context.Background(), "test prompt")

    if error != nil {
        test.Fatal(error)
    }
    if response.Content != "hello" {
        test.Errorf("expected hello, got %s", response.Content)
    }
}
```

## Integration Tests

Integration tests live in `/integration` with **task-oriented naming**.

### Naming Convention

Name files by the task being tested, not by feature:

```
integration/
├── stream_completion_test.go          <- "Stream a completion from provider"
├── agent_tool_execution_test.go       <- "Agent executes tools in a loop"
└── multi_turn_conversation_test.go    <- "Multi-turn conversation with tools"
```

### Structure

Each file tests a complete user task end-to-end:

```go
package integration

func TestStreamCompletion_FullPipeline(test *testing.T) {
    // Setup mock server or use test API key
    // Execute full streaming pipeline
    // Verify end state
}

func TestStreamCompletion_Abort(test *testing.T) {
    // Test specific aspect: aborting mid-stream
}
```

## Test Commands

```bash
# Run all tests via mage
mage test

# Run only unit tests
mage test:default

# Run tests with verbose output
mage test:verbose

# Run only integration tests
mage test:integration

# Run specific integration test
mage test:integration TestStreamCompletion
```
