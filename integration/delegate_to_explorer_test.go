package integration_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/niconiahi/the-agent/message"
	"github.com/niconiahi/the-agent/nvim"
	"github.com/niconiahi/the-agent/nvim/nvimtest"
	"github.com/niconiahi/the-agent/sender"
	"github.com/niconiahi/the-agent/tool"
)

const JOB = "map callers of Foo"

func fake_tool(name string) tool.Tool {
	return tool.NewTool(name, "fake "+name, json.RawMessage(`{"type":"object"}`),
		func(context.Context, string, map[string]any) (tool.ToolResult, error) {
			return tool.ToolResult{Content: []message.Content{message.TextContent{Text: name + " output"}}}, nil
		})
}

func every_tool() []tool.Tool {
	tools := []tool.Tool{}
	for _, name := range []string{"read", "bash_read", "edit", "write", "filter", "grep", "find", "ls", "bash_write"} {
		tools = append(tools, fake_tool(name))
	}
	return tools
}

func child_session(harness *nvimtest.Harness, folder string) string {
	return filepath.Join(filepath.Dir(nvim.SessionPath(harness.Dir, "foo")), folder, "session.md")
}

func request_of(provider *nvimtest.Provider, first_user string) []sender.LLMContext {
	matching := []sender.LLMContext{}
	for _, request := range provider.Requests() {
		for _, value := range request.Messages {
			if user, ok := value.(message.UserMessage); ok {
				if text, ok := user.Content[0].(message.TextContent); ok && (text.Text == first_user || strings.HasSuffix(text.Text, "\n\n"+first_user)) {
					matching = append(matching, request)
				}
				break
			}
		}
	}
	return matching
}

func start_delegating(t *testing.T, child ...nvimtest.Reply) (*nvimtest.Harness, *nvimtest.Provider) {
	t.Helper()
	provider := nvimtest.RegisterProvider(t,
		nvimtest.Reply{ToolCalls: []message.ToolCall{call("t1", "task", map[string]any{"role": "explorer", "job": JOB})}, TotalTokens: 5},
		nvimtest.Text("done", 10),
	)
	provider.Script(JOB, child...)
	config := nvimtest.Config()
	config.Now = fixed_clock("2026-10-06T14:32:00Z")
	config.Tools = every_tool()
	harness := nvimtest.Start(t, config)
	harness.Command("TA foo")
	harness.SetText(harness.Text() + "go\n")
	harness.Command("TASend")
	return harness, provider
}

func TestTask_RunsTheJobInANumberedChildSessionAndReturnsOnlyTheReport(t *testing.T) {
	harness, provider := start_delegating(t,
		nvimtest.Reply{ToolCalls: []message.ToolCall{call("c1", "grep", map[string]any{"pattern": "Foo"})}, TotalTokens: 6},
		nvimtest.Text("Foo is called at a.go:3", 7),
	)

	want_parent := "[system_prompt.md](../../system_prompt.md)\n\n" +
		"created · 2026-10-06T14:32:00Z\n\n" +
		"## user · 2026-10-06T14:32:00Z\n\ngo\n\n" +
		"## assistant · fake-model · 2026-10-06T14:32:00Z · 5 tokens\n\n" +
		"```tool_call id=t1 name=task ts=2026-10-06T14:32:00Z\n{\"job\":\"map callers of Foo\",\"role\":\"explorer\"}\n```\n\n" +
		"[01-map-callers-of-foo](01-map-callers-of-foo/session.md)\n\n" +
		"```tool_result id=t1 ts=2026-10-06T14:32:00Z\nFoo is called at a.go:3\n```\n\n" +
		"## assistant · fake-model · 2026-10-06T14:32:00Z · 10 tokens\n\ndone\n\n" +
		"## user\n\n"
	harness.WaitFor("the parent turn", func() bool { return session_on_disk(harness, "foo") == want_parent })

	want_child := "[system_prompt.md](../../../system_prompt.md)\n\n" +
		"created · 2026-10-06T14:32:00Z\n\n" +
		"## user · 2026-10-06T14:32:00Z\n\nmap callers of Foo\n\n" +
		"## assistant · fake-model · 2026-10-06T14:32:00Z · 6 tokens\n\n" +
		"```tool_call id=c1 name=grep ts=2026-10-06T14:32:00Z\n{\"pattern\":\"Foo\"}\n```\n\n" +
		"```tool_result id=c1 ts=2026-10-06T14:32:00Z\ngrep output\n```\n\n" +
		"## assistant · fake-model · 2026-10-06T14:32:00Z · 7 tokens\n\nFoo is called at a.go:3\n\n" +
		"## user\n\n"
	contents, error := os.ReadFile(child_session(harness, "01-map-callers-of-foo"))
	if error != nil {
		t.Fatal(error)
	}
	if string(contents) != want_child {
		t.Fatalf("child session:\n%s\nwant:\n%s", contents, want_child)
	}

	child := request_of(provider, JOB)
	if len(child) != 2 || !strings.HasPrefix(child[0].SystemPrompt, "You are a test agent.") {
		t.Fatalf("the child should run with the shared system prompt, got %d requests: %+v", len(child), child)
	}

	parent := request_of(provider, "go")
	results := []string{}
	for _, value := range parent[len(parent)-1].Messages {
		if result, ok := value.(message.ToolResultMessage); ok {
			results = append(results, result_text(result))
		}
	}
	if !slices.Equal(results, []string{"Foo is called at a.go:3"}) {
		t.Fatalf("the parent should see only the report, got %q", results)
	}
}

func TestTask_NumbersChildrenInCreationOrder(t *testing.T) {
	provider := nvimtest.RegisterProvider(t,
		nvimtest.Reply{ToolCalls: []message.ToolCall{call("t1", "task", map[string]any{"job": "find the router"})}},
		nvimtest.Reply{ToolCalls: []message.ToolCall{call("t2", "task", map[string]any{"job": "List every HTTP handler!"})}},
		nvimtest.Text("done", 10),
	)
	provider.Script("find the router", nvimtest.Text("nvim/route.go", 1))
	provider.Script("List every HTTP handler!", nvimtest.Text("none", 1))
	harness := nvimtest.Start(t, nvimtest.Config())
	send_agent_turn(harness)

	for _, folder := range []string{"01-find-the-router", "02-list-every-http-handler"} {
		if _, error := os.Stat(child_session(harness, folder)); error != nil {
			t.Errorf("child %s: %v", folder, error)
		}
	}
}

func TestTask_ExplorerGetsOnlyReadingTools(t *testing.T) {
	harness, provider := start_delegating(t, nvimtest.Text("nothing found", 7))
	harness.WaitFor("the parent turn", func() bool {
		return strings.HasSuffix(session_on_disk(harness, "foo"), "\ndone\n\n## user\n\n")
	})

	names := []string{}
	for _, schema := range request_of(provider, JOB)[0].Tools {
		names = append(names, schema.Name)
	}
	slices.Sort(names)
	if want := []string{"bash_read", "find", "grep", "ls", "read"}; !slices.Equal(names, want) {
		t.Fatalf("explorer tools: got %v, want %v", names, want)
	}
	if !slices.ContainsFunc(request_of(provider, "go")[0].Tools, func(schema sender.ToolSchema) bool { return schema.Name == "task" }) {
		t.Fatal("the root session should get the task tool")
	}
}

func TestTask_GfOnTheLinkOpensTheChild(t *testing.T) {
	harness, _ := start_delegating(t, nvimtest.Text("nothing found", 7))
	harness.WaitFor("the parent turn", func() bool {
		return strings.HasSuffix(session_on_disk(harness, "foo"), "\ndone\n\n## user\n\n")
	})
	harness.WaitFor("the parent buffer", func() bool { return strings.HasSuffix(harness.Text(), "\ndone\n\n## user\n\n") })

	harness.Command(`call search('(01-map')`)
	harness.Command("normal! l")
	harness.Command("normal! gf")
	if got, want := harness.BufferName(), child_session(harness, "01-map-callers-of-foo"); got != want {
		t.Fatalf("gf opened %s, want %s", got, want)
	}
}

func TestTAAbort_InTheParentAbortsTheRunningChild(t *testing.T) {
	harness, _ := start_delegating(t, slow(7, 150*time.Millisecond, "c1 ", "c2 ", "c3 ", "c4"))
	path := child_session(harness, "01-map-callers-of-foo")
	harness.WaitFor("the child streaming", func() bool {
		var text string
		harness.Nvim.ExecLua(`local buffer = vim.fn.bufnr(...); if buffer < 0 then return "" end; return table.concat(vim.api.nvim_buf_get_lines(buffer, 0, -1, true), "\n")`, &text, path)
		return strings.Contains(text, "c1")
	})

	harness.Command("TAAbort")

	harness.WaitFor("the child aborted on disk", func() bool {
		contents, _ := os.ReadFile(path)
		return strings.Contains(string(contents), "· aborted\n")
	})
	if contents, _ := os.ReadFile(path); strings.Contains(string(contents), "c4") {
		t.Fatalf("the child ran to the end:\n%s", contents)
	}
	var modifiable bool
	if error := harness.Nvim.ExecLua(`return vim.bo[vim.fn.bufnr(...)].modifiable`, &modifiable, path); error != nil {
		t.Fatal(error)
	}
	if !modifiable {
		t.Fatal("the aborted child is still locked")
	}
	if !session_modifiable(harness, "foo") {
		t.Fatal("the aborted parent is still locked")
	}
}
