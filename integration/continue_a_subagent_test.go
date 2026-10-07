package integration_test

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/niconiahi/the-agent/message"
	"github.com/niconiahi/the-agent/nvim"
	"github.com/niconiahi/the-agent/nvim/nvimtest"
)

const CHILD = "01-map-callers-of-foo"

func continue_child(harness *nvimtest.Harness, text string) {
	harness.T.Helper()
	harness.Command("edit " + child_session(harness, CHILD))
	harness.SetText(harness.Text() + text + "\n")
	harness.Command("TASend")
	harness.WaitFor("the child turn", func() bool {
		contents, _ := os.ReadFile(child_session(harness, CHILD))
		return strings.Count(string(contents), "## assistant") == 2 && strings.HasSuffix(string(contents), "\n## user\n\n")
	})
}

func TestTASend_ContinuingAChildAmendsTheParentsResult(t *testing.T) {
	harness, provider := start_delegating(t,
		nvimtest.Text("Foo is called at a.go:3", 7),
		nvimtest.Text("Foo is called at a.go:3 and b.go:9", 8),
	)
	wait_for_parent(harness)

	continue_child(harness, "you missed b.go")

	want := "[" + CHILD + "](" + CHILD + "/session.md)\n\n" +
		"```tool_result id=t1 amended=2026-10-06T14:32:00Z\nFoo is called at a.go:3 and b.go:9\n```\n\n" +
		"## assistant · fake-model · 2026-10-06T14:32:00Z · 10 tokens\n\ndone\n"
	harness.WaitFor("the amended parent on disk", func() bool { return strings.Contains(session_on_disk(harness, "foo"), want) })
	if got := buffer_lines(harness, nvim.SessionPath(harness.Dir, "foo")); !strings.Contains(got, want) {
		t.Fatalf("the parent buffer should show the amended result:\n%s", got)
	}

	continued := request_of(provider, JOB)
	if got, want := tool_names(continued[len(continued)-1]), []string{"bash_read", "find", "grep", "ls", "read", "task"}; !slices.Equal(got, want) {
		t.Fatalf("a continued explorer keeps its role's tools: got %v, want %v", got, want)
	}
}

func TestTASend_ContinuingAChildAmendsTheResultOfItsTaskCallWhereverItsLinkIs(t *testing.T) {
	link := "[" + CHILD + "](" + CHILD + "/session.md)\n\n"
	call := "```tool_call id=t1 name=task ts=2026-10-06T14:32:00Z\n"
	for name, move := range map[string]func(string) string{
		"removed": func(text string) string { return strings.Replace(text, link, "", 1) },
		"moved": func(text string) string {
			return strings.Replace(strings.Replace(text, link, "", 1), call, link+call, 1)
		},
	} {
		t.Run(name, func(t *testing.T) {
			harness, _ := start_delegating(t,
				nvimtest.Text("Foo is called at a.go:3", 7),
				nvimtest.Text("Foo is called at a.go:3 and b.go:9", 8),
			)
			wait_for_parent(harness)
			harness.WaitFor("the parent buffer", func() bool { return strings.HasSuffix(harness.Text(), "\ndone\n\n## user\n\n") })
			harness.SetText(move(harness.Text()))
			harness.Command("write")

			continue_child(harness, "you missed b.go")

			want := "```tool_result id=t1 amended=2026-10-06T14:32:00Z\nFoo is called at a.go:3 and b.go:9\n```\n"
			harness.WaitFor("the amended parent on disk", func() bool { return strings.Contains(session_on_disk(harness, "foo"), want) })
		})
	}
}

func TestTASend_ContinuingAChildAmendsItsOwnResultWhenTurnsReuseACallID(t *testing.T) {
	provider := nvimtest.RegisterProvider(t,
		nvimtest.Reply{ToolCalls: []message.ToolCall{call("t1", "task", map[string]any{"job": "first job"})}},
		nvimtest.Reply{ToolCalls: []message.ToolCall{call("t1", "task", map[string]any{"job": "second job"})}},
		nvimtest.Text("done", 10),
	)
	provider.Script("first job", nvimtest.Text("first report", 1), nvimtest.Text("first report, corrected", 1))
	provider.Script("second job", nvimtest.Text("second report", 1))
	harness := start_turn(t, nvimtest.Config())
	wait_for_parent(harness)

	harness.Command("edit " + child_session(harness, "01-first-job"))
	harness.SetText(harness.Text() + "again\n")
	harness.Command("TASend")

	harness.WaitFor("the amended first result", func() bool {
		return strings.Contains(session_on_disk(harness, "foo"), "amended=2026-10-06T14:32:00Z\nfirst report, corrected\n```")
	})
	if !strings.Contains(session_on_disk(harness, "foo"), "```tool_result id=t1 ts=2026-10-06T14:32:00Z\nsecond report\n```") {
		t.Fatalf("the second result should be left alone:\n%s", session_on_disk(harness, "foo"))
	}
}

func TestTASend_ContinuingAChildWhileItsParentRunsAmendsTheParentWhenItsTurnEnds(t *testing.T) {
	held := nvimtest.Reply{ToolCalls: []message.ToolCall{call("p1", "ls", map[string]any{})}, Gate: nvimtest.NewGate()}
	provider := nvimtest.RegisterProvider(t,
		nvimtest.Reply{ToolCalls: []message.ToolCall{call("t1", "task", map[string]any{"job": JOB})}},
		nvimtest.Text("done", 10),
		held,
		nvimtest.Text("done again", 11),
	)
	provider.Script(JOB, nvimtest.Text("Foo is called at a.go:3", 7), nvimtest.Text("Foo is called at a.go:3 and b.go:9", 8))
	harness := start_turn(t, nvimtest.Config())
	wait_for_parent(harness)
	harness.SetText(harness.Text() + "more\n")
	harness.Command("TASend")
	held.Gate.Step(t)

	continue_child(harness, "you missed b.go")
	amended := "```tool_result id=t1 amended=2026-10-06T14:32:00Z\nFoo is called at a.go:3 and b.go:9\n```"
	if strings.Contains(session_on_disk(harness, "foo"), amended) {
		t.Fatalf("the running parent was amended before its turn ended:\n%s", session_on_disk(harness, "foo"))
	}

	held.Gate.Step(t)
	harness.WaitFor("the amended parent after its turn", func() bool {
		text := session_on_disk(harness, "foo")
		return strings.Contains(text, amended) && strings.HasSuffix(text, "\ndone again\n\n## user\n\n")
	})
	if got := buffer_lines(harness, nvim.SessionPath(harness.Dir, "foo")); !strings.Contains(got, amended) {
		t.Fatalf("the parent buffer should show the amended result:\n%s", got)
	}
}

func TestTASend_AContinuedWorkerKeepsItsEditingTools(t *testing.T) {
	provider := nvimtest.RegisterProvider(t,
		nvimtest.Reply{ToolCalls: []message.ToolCall{call("t1", "task", map[string]any{"role": "worker", "job": JOB})}},
		nvimtest.Text("done", 10),
	)
	provider.Script(JOB, nvimtest.Text("changed a.go", 7), nvimtest.Text("changed a.go and b.go", 8))
	harness := start_turn(t, nvimtest.Config())
	wait_for_parent(harness)

	continue_child(harness, "you missed b.go")

	continued := request_of(provider, JOB)
	want := []string{"bash_read", "bash_write", "edit", "filter", "find", "grep", "ls", "read", "task", "write"}
	if got := tool_names(continued[len(continued)-1]); !slices.Equal(got, want) {
		t.Fatalf("a continued worker keeps its role's tools: got %v, want %v", got, want)
	}
}

func TestTASend_ContinuingAChildWhoseResultWasDeletedLeavesTheParentUnchanged(t *testing.T) {
	harness, _ := start_delegating(t,
		nvimtest.Text("Foo is called at a.go:3", 7),
		nvimtest.Text("Foo is called at a.go:3 and b.go:9", 8),
	)
	wait_for_parent(harness)
	harness.WaitFor("the parent buffer", func() bool { return strings.HasSuffix(harness.Text(), "\ndone\n\n## user\n\n") })

	result := "```tool_result id=t1 ts=2026-10-06T14:32:00Z\nFoo is called at a.go:3\n```\n\n"
	harness.SetText(strings.Replace(harness.Text(), result, "", 1))
	harness.Command("write")
	before := session_on_disk(harness, "foo")
	if strings.Contains(before, "tool_result") {
		t.Fatalf("the result should be deleted:\n%s", before)
	}

	continue_child(harness, "you missed b.go")

	if after := session_on_disk(harness, "foo"); after != before {
		t.Fatalf("the parent changed:\n%s\nwant:\n%s", after, before)
	}
	if got := buffer_lines(harness, nvim.SessionPath(harness.Dir, "foo")); got != before {
		t.Fatalf("the parent buffer changed:\n%s\nwant:\n%s", got, before)
	}
}
