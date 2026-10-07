package nvim

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	neovim "github.com/neovim/go-client/nvim"

	"github.com/niconiahi/the-agent/session"
)

// session_dir is the folder of the buffer's session.md, which relative
// paths in the file (the system prompt link, images) are relative to.
func session_dir(client *neovim.Nvim, buffer neovim.Buffer) (string, error) {
	name, error := client.BufferName(buffer)
	if error != nil {
		return "", error
	}
	return filepath.Dir(name), nil
}

// system_prompt reads the file the session links as its system prompt, or
// returns "" when it links none: the file is the context.
func system_prompt(parsed *session.Session, dir string) (string, error) {
	link, ok := parsed.SystemPromptLink()
	if !ok {
		return "", nil
	}
	path := link
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, link)
	}
	contents, error := os.ReadFile(path)
	if error != nil {
		return "", fmt.Errorf("system prompt %s: %w", link, error)
	}
	return strings.TrimSpace(string(contents)), nil
}
