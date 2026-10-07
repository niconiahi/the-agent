// Package nvim is the Neovim frontend: it answers the Lua plugin's RPC
// requests and turns agent runs into edits of session.md buffers.
//
// Attach registers the handlers on any go-client connection. `the-agent
// --nvim` attaches to its stdio (Neovim started it with jobstart rpc=true);
// tests attach to an embedded Neovim through nvimtest.
package nvim

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	neovim "github.com/neovim/go-client/nvim"

	"github.com/niconiahi/the-agent/model"
	"github.com/niconiahi/the-agent/sender"
	"github.com/niconiahi/the-agent/tool"
)

type Config struct {
	Model         *model.Model
	SystemPrompt  string
	Tools         []tool.Tool
	StreamOptions *sender.StreamOptions
	// Ready, when set, is checked before every send; its error is shown
	// instead of sending (e.g. a missing API key).
	Ready func() error
	// Now defaults to time.Now.
	Now func() time.Time
}

// RPC method names the Lua plugin calls with vim.rpcrequest.
const (
	METHOD_OPEN  = "the_agent_open"
	METHOD_SEND  = "the_agent_send"
	METHOD_ABORT = "the_agent_abort"
)

// NEW_SESSION is the contents of a freshly created session.md.
const NEW_SESSION = "## user\n\n"

type frontend struct {
	config  Config
	mutex   sync.Mutex
	running map[int]*turn // by buffer
}

func Attach(client *neovim.Nvim, config Config) error {
	if config.Now == nil {
		config.Now = time.Now
	}
	current := &frontend{config: config, running: map[int]*turn{}}
	return errors.Join(
		client.RegisterHandler(METHOD_OPEN, current.open),
		client.RegisterHandler(METHOD_SEND, current.send),
		client.RegisterHandler(METHOD_ABORT, current.abort),
	)
}

// SessionPath is where the session called name lives under project.
func SessionPath(project string, name string) string {
	return filepath.Join(project, ".the-agent", "sessions", strings.ReplaceAll(name, "/", "-"), "session.md")
}

func (current *frontend) open(client *neovim.Nvim, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("usage: :TA <name>")
	}

	var project string
	if error := client.Call("getcwd", &project); error != nil {
		return error
	}
	path := SessionPath(project, name)

	if _, error := os.Stat(path); errors.Is(error, os.ErrNotExist) {
		if error := os.MkdirAll(filepath.Dir(path), 0o755); error != nil {
			return error
		}
		if error := os.WriteFile(path, []byte(NEW_SESSION), 0o644); error != nil {
			return error
		}
	} else if error != nil {
		return error
	}

	var escaped string
	if error := client.Call("fnameescape", &escaped, path); error != nil {
		return error
	}
	return client.Command("edit " + escaped)
}
