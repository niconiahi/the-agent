package nvim

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	neovim "github.com/neovim/go-client/nvim"

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
