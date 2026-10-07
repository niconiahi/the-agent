package session_test

import (
	"strings"
	"testing"
	"time"

	"github.com/niconiahi/the-agent/message"
	"github.com/niconiahi/the-agent/session"
)

const text_only_session = `## user · 2026-10-06T14:32:00Z

hello, can you
explain this?

## assistant · kimi-k2.5 · 2026-10-06T14:33:00Z · 1,240 tokens

Sure. Here is a heading inside the reply:

## Not a turn

` + "```go\n## user inside a code block\n```" + `

## user · 2026-10-06T14:40:00Z

thanks
`

func TestRoundTrip_TextOnlyIsIdentity(t *testing.T) {
	cases := map[string]string{
		"full":          text_only_session,
		"empty":         "",
		"bare heading":  "## user\n\n",
		"no final eol":  "## user · 2026-10-06T14:32:00Z\n\nhi",
		"preamble kept": "some preamble\n\n## user\n\nhi\n",
	}
	for name, text := range cases {
		t.Run(name, func(t *testing.T) {
			parsed, error := session.Parse(text)
			if error != nil {
				t.Fatalf("parse: %v", error)
			}
			if got := parsed.Render(); got != text {
				t.Fatalf("round trip changed the file\nwant:\n%q\ngot:\n%q", text, got)
			}
		})
	}
}

func TestParse_ReadsRoleAndSendsTimestampsInContent(t *testing.T) {
	parsed, error := session.Parse(text_only_session)
	if error != nil {
		t.Fatalf("parse: %v", error)
	}

	messages := parsed.Messages()
	if len(messages) != 3 {
		t.Fatalf("want 3 messages, got %d", len(messages))
	}

	want := []struct {
		role string
		text string
	}{
		{"user", "2026-10-06T14:32:00Z\n\nhello, can you\nexplain this?"},
		{"assistant", "kimi-k2.5 · 2026-10-06T14:33:00Z · 1,240 tokens\n\nSure. Here is a heading inside the reply:\n\n## Not a turn\n\n```go\n## user inside a code block\n```"},
		{"user", "2026-10-06T14:40:00Z\n\nthanks"},
	}
	for index, expected := range want {
		role, text := role_and_text(t, messages[index])
		if role != expected.role {
			t.Errorf("message %d: want role %q, got %q", index, expected.role, role)
		}
		if text != expected.text {
			t.Errorf("message %d: want text\n%q\ngot\n%q", index, expected.text, text)
		}
	}
}

func TestParse_FreeFormHeadingIsIgnoredExceptRole(t *testing.T) {
	parsed, error := session.Parse("## assistant whatever I typed here\n\nok\n## user\n\nnext\n")
	if error != nil {
		t.Fatalf("parse: %v", error)
	}
	messages := parsed.Messages()
	if len(messages) != 2 {
		t.Fatalf("want 2 messages, got %d", len(messages))
	}
	role, text := role_and_text(t, messages[0])
	if role != "assistant" || text != "whatever I typed here\n\nok" {
		t.Fatalf("got %s %q", role, text)
	}
	role, text = role_and_text(t, messages[1])
	if role != "user" || text != "next" {
		t.Fatalf("got %s %q", role, text)
	}
}

func TestAppend_RendersAssistantHeadingAndNextUserHeading(t *testing.T) {
	parsed, error := session.Parse("## user · 2026-10-06T14:32:00Z\n\nhi\n")
	if error != nil {
		t.Fatalf("parse: %v", error)
	}
	parsed.AppendAssistant("kimi-k2.5", must_time(t, "2026-10-06T14:33:05Z"), 1240, "hello there")
	parsed.AppendUser()

	want := "## user · 2026-10-06T14:32:00Z\n\nhi\n\n" +
		"## assistant · kimi-k2.5 · 2026-10-06T14:33:05Z · 1,240 tokens\n\nhello there\n\n" +
		"## user\n\n"
	if got := parsed.Render(); got != want {
		t.Fatalf("want\n%q\ngot\n%q", want, got)
	}
}

func TestStampLastUser_AddsTimestampOnlyToBareHeading(t *testing.T) {
	parsed, _ := session.Parse("## user\n\nhi\n")
	if !parsed.StampLastUser(must_time(t, "2026-10-06T14:32:00Z")) {
		t.Fatal("expected bare heading to be stamped")
	}
	if got, want := parsed.Render(), "## user · 2026-10-06T14:32:00Z\n\nhi\n"; got != want {
		t.Fatalf("want %q got %q", want, got)
	}
	if parsed.StampLastUser(must_time(t, "2026-10-06T15:00:00Z")) {
		t.Fatal("an already stamped heading must not change")
	}
}

func role_and_text(t *testing.T, value message.Message) (string, string) {
	t.Helper()
	switch typed := value.(type) {
	case message.UserMessage:
		return "user", only_text(t, typed.Content)
	case message.AssistantMessage:
		return "assistant", only_text(t, typed.Content)
	default:
		t.Fatalf("unexpected message type %T", value)
		return "", ""
	}
}

func only_text(t *testing.T, content []message.Content) string {
	t.Helper()
	if len(content) != 1 {
		t.Fatalf("want 1 content block, got %d", len(content))
	}
	text, ok := content[0].(message.TextContent)
	if !ok {
		t.Fatalf("want text content, got %T", content[0])
	}
	return text.Text
}

func must_time(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, error := time.Parse(time.RFC3339, value)
	if error != nil {
		t.Fatal(error)
	}
	return parsed
}

func TestParse_RejectsToolCallWhoseArgumentsAreNotJSON(t *testing.T) {
	_, error := session.Parse("## assistant\n\n```tool_call id=tc_1 name=read ts=2026-10-06T14:33:00Z\n{\"path\": \n```\n\n```tool_result id=tc_1\nok\n```\n")
	if error == nil || !strings.Contains(error.Error(), "tc_1") {
		t.Fatalf("want an error naming tc_1, got %v", error)
	}
}

func TestAppend_RendersThinkingToolCallsAndResultsThatParseBack(t *testing.T) {
	at := must_time(t, "2026-10-06T14:33:00Z")
	parsed, _ := session.Parse("## user · 2026-10-06T14:32:00Z\n\nfix it\n")

	error := parsed.AppendAssistantMessage("kimi-k2.5", at, message.AssistantMessage{
		Content: []message.Content{
			message.ThinkingContent{Thinking: "hmm <a> & b"},
			message.TextContent{Text: "looking"},
			message.ToolCall{ID: "tc_1", Name: "read", Arguments: map[string]any{"path": "a.md"}},
			message.ToolCall{ID: "tc_2", Name: "read", Arguments: map[string]any{"path": "b.md"}},
		},
		Usage: message.Usage{TotalTokens: 1240},
	})
	if error != nil {
		t.Fatal(error)
	}
	parsed.AppendToolResult(message.ToolResultMessage{
		ToolCallID: "tc_1",
		ToolName:   "read",
		Content:    []message.Content{message.TextContent{Text: "# A\n\n```go\nx\n```"}},
	}, at)
	parsed.AppendToolResult(message.ToolResultMessage{
		ToolCallID: "tc_2",
		Content:    []message.Content{message.TextContent{Text: "no such file"}},
		IsError:    true,
	}, at)
	parsed.AppendAssistantMessage("kimi-k2.5", at, message.AssistantMessage{
		Content: []message.Content{message.TextContent{Text: "done"}},
		Usage:   message.Usage{TotalTokens: 1300},
	})
	parsed.AppendUser()

	want := "## user · 2026-10-06T14:32:00Z\n\nfix it\n\n" +
		"## assistant · kimi-k2.5 · 2026-10-06T14:33:00Z · 1,240 tokens\n\n" +
		"```thinking\nhmm <a> & b\n```\n\n" +
		"looking\n\n" +
		"```tool_call id=tc_1 name=read ts=2026-10-06T14:33:00Z\n{\"path\":\"a.md\"}\n```\n\n" +
		"```tool_call id=tc_2 name=read ts=2026-10-06T14:33:00Z\n{\"path\":\"b.md\"}\n```\n\n" +
		"````tool_result id=tc_1 ts=2026-10-06T14:33:00Z\n# A\n\n```go\nx\n```\n````\n\n" +
		"```tool_result id=tc_2 ts=2026-10-06T14:33:00Z error=true\nno such file\n```\n\n" +
		"## assistant · kimi-k2.5 · 2026-10-06T14:33:00Z · 1,300 tokens\n\ndone\n\n" +
		"## user\n\n"
	rendered := parsed.Render()
	if rendered != want {
		t.Fatalf("want\n%s\ngot\n%s", want, rendered)
	}

	reparsed, error := session.Parse(rendered)
	if error != nil {
		t.Fatalf("parse: %v", error)
	}
	messages := reparsed.Messages()
	if len(messages) != 5 {
		t.Fatalf("want 5 messages, got %d: %#v", len(messages), messages)
	}
	result, _ := messages[2].(message.ToolResultMessage)
	if got := only_text(t, result.Content); got != "called 2026-10-06T14:33:00Z · answered 2026-10-06T14:33:00Z\n\n# A\n\n```go\nx\n```" {
		t.Errorf("result with a code block: got %q", got)
	}
	failed, _ := messages[3].(message.ToolResultMessage)
	if failed.ToolCallID != "tc_2" || !failed.IsError {
		t.Errorf("error result: got %#v", messages[3])
	}
}

const tool_session = "## user · 2026-10-06T14:32:00Z\n\nfix the server\n\n" +
	"## assistant · kimi-k2.5 · 2026-10-06T14:33:00Z · 1,240 tokens\n\n" +
	"```thinking\nthe handler is registered twice…\n```\n\n" +
	"let me look\n\n" +
	"```tool_call id=tc_3 name=edit ts=2026-10-06T14:33:00Z\n{\"new_text\":\"b\",\"old_text\":\"a\",\"path\":\"server.go\"}\n```\n\n" +
	"```tool_result id=tc_3 ts=2026-10-06T14:33:01Z\nedit applied\n```\n\n" +
	"## assistant · kimi-k2.5 · 2026-10-06T14:33:02Z · 1,300 tokens\n\n" +
	"done, see:\n\n```go\nfunc main() {}\n```\n\n" +
	"## user\n\n"

func TestRoundTrip_ToolBlocksAreIdentity(t *testing.T) {
	parsed, error := session.Parse(tool_session)
	if error != nil {
		t.Fatalf("parse: %v", error)
	}
	if got := parsed.Render(); got != tool_session {
		t.Fatalf("round trip changed the file\nwant:\n%q\ngot:\n%q", tool_session, got)
	}
}

func TestMessages_ToolBlocksBecomeToolCallsResultsAndThinking(t *testing.T) {
	parsed, error := session.Parse(tool_session)
	if error != nil {
		t.Fatalf("parse: %v", error)
	}
	messages := parsed.Messages()
	if len(messages) != 4 {
		t.Fatalf("want 4 messages, got %d: %#v", len(messages), messages)
	}

	calling, ok := messages[1].(message.AssistantMessage)
	if !ok {
		t.Fatalf("message 1: want assistant, got %T", messages[1])
	}
	if len(calling.Content) != 3 {
		t.Fatalf("message 1: want thinking, text and tool call, got %#v", calling.Content)
	}
	if thinking, _ := calling.Content[0].(message.ThinkingContent); thinking.Thinking != "the handler is registered twice…" {
		t.Errorf("thinking: got %#v", calling.Content[0])
	}
	if text, _ := calling.Content[1].(message.TextContent); text.Text != "kimi-k2.5 · 2026-10-06T14:33:00Z · 1,240 tokens\n\nlet me look" {
		t.Errorf("text: got %#v", calling.Content[1])
	}
	call, _ := calling.Content[2].(message.ToolCall)
	if call.ID != "tc_3" || call.Name != "edit" || call.Arguments["path"] != "server.go" || call.Arguments["old_text"] != "a" || call.Arguments["new_text"] != "b" {
		t.Errorf("tool call: got %#v", calling.Content[2])
	}
	if calling.StopReason != message.STOP_REASON_TOOL_USE {
		t.Errorf("stop reason: got %q", calling.StopReason)
	}

	result, ok := messages[2].(message.ToolResultMessage)
	if !ok {
		t.Fatalf("message 2: want tool result, got %T", messages[2])
	}
	if result.ToolCallID != "tc_3" || result.ToolName != "edit" || result.IsError {
		t.Errorf("tool result: got %#v", result)
	}
	if got := only_text(t, result.Content); got != "called 2026-10-06T14:33:00Z · answered 2026-10-06T14:33:01Z\n\nedit applied" {
		t.Errorf("tool result text: got %q", got)
	}

	role, text := role_and_text(t, messages[3])
	if role != "assistant" || text != "kimi-k2.5 · 2026-10-06T14:33:02Z · 1,300 tokens\n\ndone, see:\n\n```go\nfunc main() {}\n```" {
		t.Errorf("message 3: got %s %q", role, text)
	}
}

func TestParse_DeletedToolResultRemovesItsCallFromMessagesAndFile(t *testing.T) {
	edited := strings.Replace(tool_session, "```tool_result id=tc_3 ts=2026-10-06T14:33:01Z\nedit applied\n```\n\n", "", 1)
	want := "## user · 2026-10-06T14:32:00Z\n\nfix the server\n\n" +
		"## assistant · kimi-k2.5 · 2026-10-06T14:33:00Z · 1,240 tokens\n\n" +
		"```thinking\nthe handler is registered twice…\n```\n\n" +
		"let me look\n\n" +
		"## assistant · kimi-k2.5 · 2026-10-06T14:33:02Z · 1,300 tokens\n\n" +
		"done, see:\n\n```go\nfunc main() {}\n```\n\n" +
		"## user\n\n"

	parsed, error := session.Parse(edited)
	if error != nil {
		t.Fatalf("parse: %v", error)
	}
	if got := parsed.Render(); got != edited {
		t.Fatalf("parse alone must not change the file\nwant:\n%q\ngot:\n%q", edited, got)
	}
	if !parsed.Repair() {
		t.Fatal("Repair must report the removed tool_call")
	}
	if got := parsed.Render(); got != want {
		t.Fatalf("file\nwant:\n%q\ngot:\n%q", want, got)
	}
	reparsed, _ := session.Parse(edited)
	for _, value := range reparsed.Messages() {
		assistant, ok := value.(message.AssistantMessage)
		if !ok {
			continue
		}
		for _, content := range assistant.Content {
			if _, ok := content.(message.ToolCall); ok {
				t.Fatalf("orphaned tool call was sent: %#v", assistant)
			}
		}
		if assistant.StopReason == message.STOP_REASON_TOOL_USE {
			t.Errorf("stop reason still tool_use: %#v", assistant)
		}
	}
}

func TestParse_DeletedToolCallRemovesItsResultFromMessagesAndFile(t *testing.T) {
	edited := strings.Replace(tool_session, "```tool_call id=tc_3 name=edit ts=2026-10-06T14:33:00Z\n{\"new_text\":\"b\",\"old_text\":\"a\",\"path\":\"server.go\"}\n```\n\n", "", 1)
	want := strings.Replace(edited, "```tool_result id=tc_3 ts=2026-10-06T14:33:01Z\nedit applied\n```\n\n", "", 1)

	parsed, error := session.Parse(edited)
	if error != nil {
		t.Fatalf("parse: %v", error)
	}
	for _, value := range parsed.Messages() {
		if result, ok := value.(message.ToolResultMessage); ok {
			t.Fatalf("orphaned tool result was sent: %#v", result)
		}
	}
	if !parsed.Repair() {
		t.Fatal("Repair must report the removed tool_result")
	}
	if got := parsed.Render(); got != want {
		t.Fatalf("file\nwant:\n%q\ngot:\n%q", want, got)
	}
}

func TestRepair_EditedOrDeletedThinkingIsNeverRepaired(t *testing.T) {
	cases := map[string]string{
		"edited":  strings.Replace(tool_session, "the handler is registered twice…", "nope, it is fine", 1),
		"deleted": strings.Replace(tool_session, "```thinking\nthe handler is registered twice…\n```\n\n", "", 1),
		"emptied": strings.Replace(tool_session, "the handler is registered twice…\n", "", 1),
	}
	for name, text := range cases {
		t.Run(name, func(t *testing.T) {
			parsed, error := session.Parse(text)
			if error != nil {
				t.Fatalf("parse: %v", error)
			}
			if parsed.Repair() {
				t.Error("Repair must not report a thinking edit")
			}
			if got := parsed.Render(); got != text {
				t.Fatalf("thinking edit was repaired\nwant:\n%q\ngot:\n%q", text, got)
			}
		})
	}
}
