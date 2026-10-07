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
	"github.com/niconiahi/the-agent/session"
	"github.com/niconiahi/the-agent/tool"
)

type Config struct {
	Project string
	Model   *model.Model

	SystemPrompt  string
	Tools         []tool.Tool
	StreamOptions *sender.StreamOptions

	Ready func() error

	Now func() time.Time
}

const (
	METHOD_OPEN  = "the_agent_open"
	METHOD_SEND  = "the_agent_send"
	METHOD_ABORT = "the_agent_abort"

	METHOD_COUNT = "the_agent_count"
)

const NEW_SESSION = session.SYSTEM_PROMPT_LINK + "\n\n## user\n\n"

func SystemPromptPath(project string) string {
	return filepath.Join(project, ".the-agent", "system_prompt.md")
}

type frontend struct {
	config  Config
	mutex   sync.Mutex
	running map[int]*turn
}

func Attach(client *neovim.Nvim, config Config) error {
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.Project == "" {
		project, error := os.Getwd()
		if error != nil {
			return error
		}
		config.Project = project
	}
	current := &frontend{config: config, running: map[int]*turn{}}
	return errors.Join(
		client.RegisterHandler(METHOD_OPEN, current.open),
		client.RegisterHandler(METHOD_SEND, current.send),
		client.RegisterHandler(METHOD_ABORT, current.abort),
		client.RegisterHandler(METHOD_COUNT, current.refresh),
	)
}

func SessionPath(project string, name string) string {
	return filepath.Join(project, ".the-agent", "sessions", strings.ReplaceAll(name, "/", "-"), "session.md")
}

func (current *frontend) open(client *neovim.Nvim, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("usage: :TA <name>")
	}

	project := current.config.Project
	path := SessionPath(project, name)

	if error := current.seed_system_prompt(project); error != nil {
		return error
	}

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

func (current *frontend) seed_system_prompt(project string) error {
	path := SystemPromptPath(project)
	if _, error := os.Stat(path); !errors.Is(error, os.ErrNotExist) {
		return error
	}
	if error := os.MkdirAll(filepath.Dir(path), 0o755); error != nil {
		return error
	}
	return os.WriteFile(path, []byte(strings.TrimSpace(current.config.SystemPrompt)+"\n"), 0o644)
}
