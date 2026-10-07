package integration_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/niconiahi/the-agent/message"
	"github.com/niconiahi/the-agent/nvim"
	"github.com/niconiahi/the-agent/nvim/nvimtest"
	"github.com/niconiahi/the-agent/sender"
)

func tool_names(request sender.LLMContext) []string {
	names := []string{}
	for _, schema := range request.Tools {
		names = append(names, schema.Name)
	}
	slices.Sort(names)
	return names
}

func start_turn(t *testing.T, config nvim.Config) *nvimtest.Harness {
	t.Helper()
	config.Now = fixed_clock("2026-10-06T14:32:00Z")
	if config.Tools == nil {
		config.Tools = every_tool()
	}
	harness := nvimtest.Start(t, config)
	harness.Command("TA foo")
	harness.SetText(harness.Text() + "go\n")
	harness.Command("TASend")
	return harness
}

func wait_for_parent(harness *nvimtest.Harness) {
	harness.T.Helper()
	harness.WaitFor("the parent turn", func() bool {
		return strings.HasSuffix(session_on_disk(harness, "foo"), "\ndone\n\n## user\n\n")
	})
}

func TestTask_WorkerGetsTheEditingTools(t *testing.T) {
	provider := nvimtest.RegisterProvider(t,
		nvimtest.Reply{ToolCalls: []message.ToolCall{call("t1", "task", map[string]any{"role": "worker", "job": "rewrite the middleware"})}},
		nvimtest.Text("done", 10),
	)
	provider.Script("rewrite the middleware", nvimtest.Text("changed middleware.go", 7))
	harness := start_turn(t, nvimtest.Config())
	wait_for_parent(harness)

	got := tool_names(request_of(provider, "rewrite the middleware")[0])
	want := []string{"bash_read", "bash_write", "edit", "filter", "find", "grep", "ls", "read", "task", "write"}
	if !slices.Equal(got, want) {
		t.Fatalf("worker tools: got %v, want %v", got, want)
	}
}

func TestTask_TwoTasksInOneTurnRunConcurrently(t *testing.T) {
	provider := nvimtest.RegisterProvider(t,
		nvimtest.Reply{ToolCalls: []message.ToolCall{
			call("t1", "task", map[string]any{"job": "first job"}),
			call("t2", "task", map[string]any{"job": "second job"}),
		}},
		nvimtest.Text("done", 10),
	)
	gate := nvimtest.NewGate()
	provider.Script("first job",
		nvimtest.Reply{ToolCalls: []message.ToolCall{call("c1", "ls", map[string]any{})}, Gate: gate},
		nvimtest.Text("first report", 1),
	)
	provider.Script("second job", nvimtest.Text("second report", 1))
	harness := start_turn(t, nvimtest.Config())

	harness.WaitFor("the second child to start while the first is held", func() bool {
		return len(request_of(provider, "second job")) == 1
	})
	gate.Step(t)
	gate.Step(t)
	wait_for_parent(harness)

	parent := request_of(provider, "go")
	results := []string{}
	for _, value := range parent[len(parent)-1].Messages {
		if result, ok := value.(message.ToolResultMessage); ok {
			results = append(results, result_text(result))
		}
	}
	if !slices.Equal(results, []string{"first report", "second report"}) {
		t.Fatalf("both reports should return to the parent, got %q", results)
	}
}

func has_task(request sender.LLMContext) bool {
	return slices.Contains(tool_names(request), "task")
}

func TestTask_AnAgentAtTheDefaultDepthLimitGetsNoTask(t *testing.T) {
	provider := nvimtest.RegisterProvider(t,
		nvimtest.Reply{ToolCalls: []message.ToolCall{call("t1", "task", map[string]any{"job": "level one"})}},
		nvimtest.Text("done", 10),
	)
	provider.Script("level one",
		nvimtest.Reply{ToolCalls: []message.ToolCall{call("t2", "task", map[string]any{"job": "level two"})}},
		nvimtest.Text("one", 1),
	)
	provider.Script("level two",
		nvimtest.Reply{ToolCalls: []message.ToolCall{call("t3", "task", map[string]any{"job": "level three"})}},
		nvimtest.Text("two", 1),
	)
	provider.Script("level three", nvimtest.Text("three", 1))
	harness := start_turn(t, nvimtest.Config())
	wait_for_parent(harness)

	if !has_task(request_of(provider, "level two")[0]) {
		t.Fatal("an agent at depth 2 should get task")
	}
	if has_task(request_of(provider, "level three")[0]) {
		t.Fatal("an agent at depth 3 should not get task")
	}
}

func TestTask_AnAgentAtTheConfiguredDepthLimitGetsNoTask(t *testing.T) {
	provider := nvimtest.RegisterProvider(t,
		nvimtest.Reply{ToolCalls: []message.ToolCall{call("t1", "task", map[string]any{"role": "worker", "job": "level one"})}},
		nvimtest.Text("done", 10),
	)
	provider.Script("level one", nvimtest.Text("one", 1))
	config := nvimtest.Config()
	config.MaxDepth = 1
	harness := start_turn(t, config)
	wait_for_parent(harness)

	if has_task(request_of(provider, "level one")[0]) {
		t.Fatal("an agent at the configured depth limit should not get task")
	}
	if !has_task(request_of(provider, "go")[0]) {
		t.Fatal("the root session should still get task")
	}
}

func task_roles(t *testing.T, request sender.LLMContext) []string {
	t.Helper()
	for _, schema := range request.Tools {
		if schema.Name != "task" {
			continue
		}
		var parameters struct {
			Properties struct {
				Role struct {
					Enum []string `json:"enum"`
				} `json:"role"`
			} `json:"properties"`
		}
		if error := json.Unmarshal(schema.Parameters, &parameters); error != nil {
			t.Fatal(error)
		}
		return parameters.Properties.Role.Enum
	}
	t.Fatal("no task tool")
	return nil
}

func TestTask_AnExplorerCanOnlyDelegateToExplorers(t *testing.T) {
	provider := nvimtest.RegisterProvider(t,
		nvimtest.Reply{ToolCalls: []message.ToolCall{call("t1", "task", map[string]any{"job": "look around"})}},
		nvimtest.Text("done", 10),
	)
	provider.Script("look around",
		nvimtest.Reply{ToolCalls: []message.ToolCall{call("t2", "task", map[string]any{"role": "worker", "job": "change things"})}},
		nvimtest.Text("looked", 1),
	)
	harness := start_turn(t, nvimtest.Config())
	wait_for_parent(harness)

	if got := task_roles(t, request_of(provider, "go")[0]); !slices.Equal(got, []string{"explorer", "worker"}) {
		t.Fatalf("the root session's task roles: %v", got)
	}
	if got := task_roles(t, request_of(provider, "look around")[0]); !slices.Equal(got, []string{"explorer"}) {
		t.Fatalf("an explorer's task roles: %v", got)
	}
	refused := result_of(t, provider, "t2")
	if !refused.IsError || result_text(refused) != `unknown role "worker": use explorer` {
		t.Fatalf("an explorer starting a worker: %q (error %v)", result_text(refused), refused.IsError)
	}
	if len(request_of(provider, "change things")) != 0 {
		t.Fatal("no worker should have run")
	}
}
