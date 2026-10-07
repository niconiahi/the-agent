package message

import "strings"

type Content interface {
	is_content()
}

func Text(contents []Content) string {
	parts := []string{}
	for _, content := range contents {
		if text, ok := content.(TextContent); ok {
			parts = append(parts, text.Text)
		}
	}
	return strings.Join(parts, "\n\n")
}

type TextContent struct {
	Text          string `json:"text"`
	TextSignature string `json:"text_signature,omitempty"`
}

func (TextContent) is_content() {}

type ThinkingContent struct {
	Thinking          string `json:"thinking"`
	ThinkingSignature string `json:"thinking_signature,omitempty"`
	Redacted          bool   `json:"redacted,omitempty"`
}

func (ThinkingContent) is_content() {}

type ImageContent struct {
	Data     string `json:"data"`
	MimeType string `json:"mime_type"`
}

func (ImageContent) is_content() {}

type ToolCall struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

func (ToolCall) is_content() {}
