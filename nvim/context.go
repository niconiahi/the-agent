package nvim

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	neovim "github.com/neovim/go-client/nvim"

	"github.com/niconiahi/the-agent/message"
	"github.com/niconiahi/the-agent/session"
)

func session_dir(client *neovim.Nvim, buffer neovim.Buffer) (string, error) {
	name, error := client.BufferName(buffer)
	if error != nil {
		return "", error
	}
	return filepath.Dir(name), nil
}

func system_prompt(parsed *session.Session, directory string) (string, error) {
	link, ok := parsed.SystemPromptLink()
	if !ok {
		return "", nil
	}
	path := link
	if !filepath.IsAbs(path) {
		path = filepath.Join(directory, link)
	}
	contents, error := os.ReadFile(path)
	if error != nil {
		return "", fmt.Errorf("system prompt %s: %w", link, error)
	}
	return strings.TrimSpace(string(contents)), nil
}

type request struct {
	buffer    neovim.Buffer
	directory string
	text      string
	parsed    *session.Session
	prompt    string
	messages  []message.Message
	size      tokens
	missing   error
}

func (current *frontend) prepare(client *neovim.Nvim, buffer neovim.Buffer) (*request, error) {
	text, error := buffer_text(client, buffer)
	if error != nil {
		return nil, error
	}
	parsed, error := session.Parse(text)
	if error != nil {
		return nil, error
	}
	parsed.StampLastUser(current.config.Now())
	directory, error := session_dir(client, buffer)
	if error != nil {
		return nil, error
	}
	prepared := &request{buffer: buffer, directory: directory, text: text, parsed: parsed, messages: parsed.Messages()}
	prompt, prompt_error := system_prompt(parsed, directory)
	prepared.prompt = parsed.SystemPrompt(prompt)
	loaded, image_error := session.LoadImages(prepared.messages, directory)
	if image_error == nil {
		prepared.messages = loaded
	}
	prepared.missing = errors.Join(prompt_error, image_error)
	if prepared.size, error = current.count(client, prepared.prompt, prepared.messages); error != nil {
		return nil, error
	}
	return prepared, nil
}

func (prepared *request) history() []message.Message {
	return prepared.messages[:len(prepared.messages)-1]
}

func (prepared *request) last() (message.UserMessage, bool) {
	if len(prepared.messages) == 0 {
		return message.UserMessage{}, false
	}
	last, ok := prepared.messages[len(prepared.messages)-1].(message.UserMessage)
	return last, ok
}
