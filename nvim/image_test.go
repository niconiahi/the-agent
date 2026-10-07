package nvim_test

import (
	"encoding/base64"
	"reflect"
	"strings"
	"testing"

	"github.com/niconiahi/the-agent/message"
	"github.com/niconiahi/the-agent/nvim/nvimtest"
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
