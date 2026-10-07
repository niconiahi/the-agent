package integration_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/niconiahi/the-agent/message"
	"github.com/niconiahi/the-agent/nvim/nvimtest"
	"github.com/niconiahi/the-agent/tool"
)

const PIXEL = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYAAAAAYAAjCB0C8AAAAASUVORK5CYII="

func TestTASend_SendsReferencedSiblingImage(t *testing.T) {
	provider := nvimtest.RegisterProvider(t, nvimtest.Text("a pixel", 10))
	harness := nvimtest.Start(t, nvimtest.Config())
	pixel, _ := base64.StdEncoding.DecodeString(PIXEL)
	harness.WriteFile(".the-agent/sessions/foo/shot.png", string(pixel))

	harness.Command("TA foo")
	harness.SetText(harness.Text() + "what is this? ![shot](shot.png)\n")
	harness.Command("TASend")
	harness.WaitFor("the reply on disk", func() bool { return strings.Contains(on_disk(harness), "a pixel") })

	content := provider.Requests()[0].Messages[0].(message.UserMessage).Content
	if len(content) != 2 || !reflect.DeepEqual(content[1], message.ImageContent{Data: PIXEL, MimeType: "image/png"}) {
		t.Fatalf("want the text and the image, got %#v", content)
	}
	if file := harness.ReadFile(SESSION); strings.Contains(file, PIXEL) || !strings.Contains(file, "![shot](shot.png)") {
		t.Fatalf("session.md must keep the reference, never base64:\n%s", file)
	}
}

func TestTASend_ToolResultImageIsSavedBesideTheSessionAndSentNextTurn(t *testing.T) {
	image := message.ImageContent{Data: PIXEL, MimeType: "image/png"}
	screenshot := tool.NewTool("screenshot", "take a screenshot", json.RawMessage(`{"type":"object"}`),
		func(_ context.Context, _ string, _ map[string]any) (tool.ToolResult, error) {
			return tool.ToolResult{Content: []message.Content{message.TextContent{Text: "the page:"}, image}}, nil
		})
	provider := nvimtest.RegisterProvider(t,
		nvimtest.Reply{ToolCalls: []message.ToolCall{{ID: "tc_1", Name: "screenshot"}}, TotalTokens: 10},
		nvimtest.Text("looks fine", 20),
		nvimtest.Text("you're welcome", 30),
	)
	config := nvimtest.Config()
	config.Tools = []tool.Tool{screenshot}
	harness := nvimtest.Start(t, config)

	harness.Command("TA foo")
	harness.SetText(harness.Text() + "how does it look?\n")
	harness.Command("TASend")
	harness.WaitFor("the turn on disk", func() bool { return strings.Contains(on_disk(harness), "looks fine") })

	saved, _ := filepath.Glob(filepath.Join(harness.Dir, ".the-agent", "sessions", "foo", "*.png"))
	if len(saved) != 1 {
		t.Fatalf("want the image saved beside session.md, got %v", saved)
	}
	file := harness.ReadFile(SESSION)
	if strings.Contains(file, PIXEL) || !strings.Contains(file, "the page:\n![]("+filepath.Base(saved[0])+")") {
		t.Fatalf("the tool result must reference the saved image, never base64:\n%s", file)
	}

	harness.SetText(harness.Text() + "thanks\n")
	harness.Command("TASend")
	harness.WaitFor("the second reply", func() bool { return strings.Contains(on_disk(harness), "you're welcome") })

	found := false
	for _, value := range provider.Requests()[2].Messages {
		if result, ok := value.(message.ToolResultMessage); ok {
			for _, content := range result.Content {
				if reflect.DeepEqual(content, message.Content(image)) {
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatalf("the next send must carry the image in the tool result, got %#v", provider.Requests()[2].Messages)
	}
}

func TestTASend_RefusesWhenReferencedImageIsMissing(t *testing.T) {
	provider := nvimtest.RegisterProvider(t)
	harness := nvimtest.Start(t, nvimtest.Config())

	harness.Command("TA foo")
	harness.SetText(harness.Text() + "![shot](gone.png)\n")
	error := harness.CommandError("TASend")

	if error == nil || !strings.Contains(error.Error(), "gone.png") {
		t.Fatalf("want an error naming the image, got %v", error)
	}
	if len(provider.Requests()) != 0 {
		t.Fatal("nothing must be sent")
	}
}
