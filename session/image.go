package session

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/niconiahi/the-agent/message"
)

// Images live in files next to session.md and the session refers to them by
// relative path, ![alt](name.png); base64 never enters the file.

var image_extensions = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
}

var image_pattern = regexp.MustCompile(`!\[[^\]]*\]\(([^)\s]+)\)`)

// SaveImage writes image to a file in dir named after its contents and
// returns the markdown reference to put in the session.
func SaveImage(dir string, image message.ImageContent) (string, error) {
	data, error := base64.StdEncoding.DecodeString(image.Data)
	if error != nil {
		return "", fmt.Errorf("image: %w", error)
	}
	extension := ""
	for candidate, mime := range image_extensions {
		if mime == image.MimeType && (extension == "" || len(candidate) < len(extension)) {
			extension = candidate
		}
	}
	if extension == "" {
		return "", fmt.Errorf("image: unsupported type %q", image.MimeType)
	}
	sum := sha256.Sum256(data)
	name := "image-" + hex.EncodeToString(sum[:6]) + extension
	if error := os.WriteFile(filepath.Join(dir, name), data, 0o644); error != nil {
		return "", error
	}
	return "![](" + name + ")", nil
}

// LoadImages returns messages with every relative image reference in a user
// or tool result message followed by the image it points to, read from dir.
// The reference text stays, so the model sees which file it is looking at.
// References inside fenced blocks, remote URLs and absolute paths are left
// alone; a referenced file that is missing is an error.
func LoadImages(messages []message.Message, dir string) ([]message.Message, error) {
	loaded := make([]message.Message, len(messages))
	for index, current := range messages {
		var error error
		switch typed := current.(type) {
		case message.UserMessage:
			typed.Content, error = load_content(typed.Content, dir)
			loaded[index] = typed
		case message.ToolResultMessage:
			typed.Content, error = load_content(typed.Content, dir)
			loaded[index] = typed
		default:
			loaded[index] = current
		}
		if error != nil {
			return nil, error
		}
	}
	return loaded, nil
}

func load_content(contents []message.Content, dir string) ([]message.Content, error) {
	loaded := []message.Content{}
	for _, content := range contents {
		text, ok := content.(message.TextContent)
		if !ok {
			loaded = append(loaded, content)
			continue
		}
		split, error := split_images(text.Text, dir)
		if error != nil {
			return nil, error
		}
		loaded = append(loaded, split...)
	}
	return loaded, nil
}

// split_images cuts text after each image reference and puts the image there.
func split_images(text string, dir string) ([]message.Content, error) {
	contents := []message.Content{}
	start := 0
	for _, reference := range image_references(text) {
		image, error := read_image(dir, text[reference[2]:reference[3]])
		if error != nil {
			return nil, error
		}
		contents = append(contents, message.TextContent{Text: text[start:reference[1]]}, image)
		start = reference[1]
	}
	if start < len(text) || len(contents) == 0 {
		contents = append(contents, message.TextContent{Text: text[start:]})
	}
	return contents, nil
}

// image_references finds local image references outside fenced blocks, as
// submatch offsets into text.
func image_references(text string) [][]int {
	references := [][]int{}
	in_fence := false
	offset := 0
	for _, line := range strings.SplitAfter(text, "\n") {
		if strings.HasPrefix(strings.TrimLeft(line, " "), "```") {
			in_fence = !in_fence
		} else if !in_fence {
			for _, match := range image_pattern.FindAllStringSubmatchIndex(line, -1) {
				target := line[match[2]:match[3]]
				if local_image(target) {
					for index := range match {
						match[index] += offset
					}
					references = append(references, match)
				}
			}
		}
		offset += len(line)
	}
	return references
}

func local_image(target string) bool {
	if strings.Contains(target, "://") || strings.HasPrefix(target, "data:") || filepath.IsAbs(target) {
		return false
	}
	_, ok := image_extensions[strings.ToLower(filepath.Ext(target))]
	return ok
}

func read_image(dir string, target string) (message.ImageContent, error) {
	data, error := os.ReadFile(filepath.Join(dir, target))
	if errors.Is(error, os.ErrNotExist) {
		return message.ImageContent{}, fmt.Errorf("image %s: not found next to session.md", target)
	}
	if error != nil {
		return message.ImageContent{}, fmt.Errorf("image %s: %w", target, error)
	}
	return message.ImageContent{
		Data:     base64.StdEncoding.EncodeToString(data),
		MimeType: image_extensions[strings.ToLower(filepath.Ext(target))],
	}, nil
}
