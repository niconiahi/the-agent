package nvim

import (
	"errors"
	"path/filepath"

	neovim "github.com/neovim/go-client/nvim"

	"github.com/niconiahi/the-agent/orchestrator"
	"github.com/niconiahi/the-agent/subagent"
	"github.com/niconiahi/the-agent/tool"
)

// host streams each subagent into its own session.md, the way send streams a
// root session, without taking over any window: gf on the link in the
// parent's file opens it.
type host struct {
	frontend *frontend
	client   *neovim.Nvim
}

func (current *frontend) tools(client *neovim.Nvim) []tool.Tool {
	return append(append([]tool.Tool{}, current.config.Tools...), subagent.Task(subagent.Config{
		Model:         current.config.Model,
		StreamOptions: current.config.StreamOptions,
		Tools:         current.config.Tools,
		SystemPrompt:  SystemPromptPath(current.config.Project),
		Host:          host{frontend: current, client: client},
		Now:           current.config.Now,
	}))
}

// Open loads the child's session.md into a buffer and routes the child's
// events, which carry the child's ID (its session directory), into it. They
// reach the router straight from the child agent, never through the parent,
// which would stamp them with its own ID.
func (current host) Open(path string) (func(orchestrator.AgentEvent), func(error), error) {
	var number int
	if error := current.client.ExecLua(`
		local buffer = vim.fn.bufadd(...)
		vim.fn.bufload(buffer)
		vim.bo[buffer].buflisted = true
		return buffer
	`, &number, path); error != nil {
		return nil, nil, error
	}
	buffer := neovim.Buffer(number)
	text, error := buffer_text(current.client, buffer)
	if error != nil {
		return nil, nil, error
	}
	output, error := start_stream(current.client, buffer, text, text)
	if error != nil {
		return nil, nil, error
	}
	directory := filepath.Dir(path)
	frontend := current.frontend
	replies := &reply_writer{output: output, directory: directory, model: frontend.config.Model.ID, now: frontend.config.Now}
	remove := frontend.routes.add(directory, replies.handle)
	return frontend.routes.route, current.end(replies, remove), nil
}

// end stops routing the child's events and closes its stream the way send
// closes a root turn: a fresh ## user heading, unlocked and saved. The child's
// own error reaches the parent as the tool result, so it isn't notified here.
func (current host) end(replies *reply_writer, remove func()) func(error) {
	return func(error) {
		remove()
		if replies.wrote {
			replies.output.begin("## user\n")
		}
		if failure := errors.Join(replies.failure, replies.output.finish()); failure != nil {
			notify(current.client, failure.Error(), LOG_LEVEL_ERROR)
		}
	}
}
