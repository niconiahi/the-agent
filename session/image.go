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

var image_extensions = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
}

var image_pattern = regexp.MustCompile(`!\[[^\]]*\]\(([^)\s]+)\)`)

func SaveImage(directory string, image message.ImageContent) (string, error) {
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
	if error := os.WriteFile(filepath.Join(directory, name), data, 0o644); error != nil {
		return "", error
	}
	return "![](" + name + ")", nil
}

func LoadImages(messages []message.Message, directory string) ([]message.Message, error) {
	loaded := make([]message.Message, len(messages))
	for index, current := range messages {
		var error error
		switch typed := current.(type) {
		case message.UserMessage:
			typed.Content, error = load_content(typed.Content, directory)
			loaded[index] = typed
		case message.ToolResultMessage:
			typed.Content, error = load_content(typed.Content, directory)
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

func load_content(contents []message.Content, directory string) ([]message.Content, error) {
	loaded := []message.Content{}
	for _, content := range contents {
		text, ok := content.(message.TextContent)
		if !ok {
			loaded = append(loaded, content)
			continue
		}
		split, error := split_images(text.Text, directory)
		if error != nil {
			return nil, error
		}
		loaded = append(loaded, split...)
	}
	return loaded, nil
}

func split_images(text string, directory string) ([]message.Content, error) {
	contents := []message.Content{}
	start := 0
	for _, reference := range image_references(text) {
		image, error := read_image(directory, text[reference[2]:reference[3]])
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

func read_image(directory string, target string) (message.ImageContent, error) {
	data, error := os.ReadFile(filepath.Join(directory, target))
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
