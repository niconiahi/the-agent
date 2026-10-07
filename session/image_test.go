package session_test

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/niconiahi/the-agent/message"
	"github.com/niconiahi/the-agent/session"
)

// A 1x1 transparent PNG.
const PIXEL = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYAAAAAYAAjCB0C8AAAAASUVORK5CYII="

func load(t *testing.T, dir string, text string) []message.Message {
	t.Helper()
	parsed, error := session.Parse(text)
	if error != nil {
		t.Fatal(error)
	}
	messages, error := session.LoadImages(parsed.Messages(), dir)
	if error != nil {
		t.Fatal(error)
	}
	return messages
}

func TestImages_RoundTripAsRelativeReferencesToSiblingFiles(t *testing.T) {
	dir := t.TempDir()
	image := message.ImageContent{Data: PIXEL, MimeType: "image/png"}

	reference, error := session.SaveImage(dir, image)
	if error != nil {
		t.Fatal(error)
	}
	if strings.Contains(reference, PIXEL) || strings.Contains(reference, "/") {
		t.Fatalf("want a relative reference to a sibling file, got %q", reference)
	}
	saved, _ := filepath.Glob(filepath.Join(dir, "*.png"))
	if len(saved) != 1 {
		t.Fatalf("want the image saved next to session.md, got %v", saved)
	}

	messages := load(t, dir, "## user\n\nlook at this "+reference+" please\n")

	want := []message.Content{
		message.TextContent{Text: "look at this " + reference},
		image,
		message.TextContent{Text: " please"},
	}
	if got := messages[0].(message.UserMessage).Content; !reflect.DeepEqual(got, want) {
		t.Fatalf("want %#v\ngot  %#v", want, got)
	}
}

func TestSaveImage_SameImageSameFile(t *testing.T) {
	dir := t.TempDir()
	image := message.ImageContent{Data: PIXEL, MimeType: "image/png"}

	first, _ := session.SaveImage(dir, image)
	second, _ := session.SaveImage(dir, image)

	if first != second {
		t.Fatalf("want one file per image, got %q and %q", first, second)
	}
}

func TestLoadImages_ReadsHandWrittenReferences(t *testing.T) {
	dir := t.TempDir()
	pixel, _ := base64.StdEncoding.DecodeString(PIXEL)
	os.MkdirAll(filepath.Join(dir, "shots"), 0o755)
	os.WriteFile(filepath.Join(dir, "shots", "login.jpg"), pixel, 0o644)

	messages := load(t, dir, "## user\n\n![the login page](shots/login.jpg)\n")

	content := messages[0].(message.UserMessage).Content
	if len(content) != 2 {
		t.Fatalf("want the reference text and the image, got %#v", content)
	}
	if got := content[1]; !reflect.DeepEqual(got, message.ImageContent{Data: PIXEL, MimeType: "image/jpeg"}) {
		t.Fatalf("got %#v", got)
	}
}

func TestLoadImages_LeavesOtherLinksAlone(t *testing.T) {
	text := "## user\n\n![remote](https://example.com/a.png) [a link](notes.png)\n\n```\n![in a fence](missing.png)\n```\n\n" +
		"## assistant\n\n![assistants don't send images](missing.png)\n"

	messages := load(t, t.TempDir(), text)

	for index, current := range messages {
		var content []message.Content
		switch typed := current.(type) {
		case message.UserMessage:
			content = typed.Content
		case message.AssistantMessage:
			content = typed.Content
		}
		if len(content) != 1 {
			t.Fatalf("message %d: want text only, got %#v", index, content)
		}
	}
}

func TestLoadImages_MissingFileIsAnError(t *testing.T) {
	parsed, _ := session.Parse("## user\n\n![gone](gone.png)\n")

	_, error := session.LoadImages(parsed.Messages(), t.TempDir())

	if error == nil || !strings.Contains(error.Error(), "gone.png") {
		t.Fatalf("want an error naming the file, got %v", error)
	}
}
