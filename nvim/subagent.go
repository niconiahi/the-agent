package nvim

import (
	"path/filepath"

	neovim "github.com/neovim/go-client/nvim"

	"github.com/niconiahi/the-agent/orchestrator"
	"github.com/niconiahi/the-agent/subagent"
	"github.com/niconiahi/the-agent/tool"
)

type host struct {
	frontend *frontend
	client   *neovim.Nvim
}

func (current *frontend) tools(client *neovim.Nvim) []tool.Tool {
	return append(append([]tool.Tool{}, current.config.Tools...), subagent.Task(subagent.Config{
		Model:         current.config.Model,
		StreamOptions: current.config.StreamOptions,
		Tools:         current.config.Tools,
		ExplorerTools: current.config.ExplorerTools,
		MaxDepth:      current.config.MaxDepth,
		SystemPrompt:  SystemPromptPath(current.config.Project),
		Host:          host{frontend: current, client: client},
		Now:           current.config.Now,
	}))
}

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
	writer := current.frontend.start_writer(current.client, output, directory)
	remove := current.frontend.routes.add(directory, writer.handle)
	return current.frontend.routes.route, current.end(writer, remove), nil
}

func (current host) end(writer *session_writer, remove func()) func(error) {
	return func(error) {
		remove()
		if failure := writer.finish(); failure != nil {
			notify(current.client, failure.Error(), LOG_LEVEL_ERROR)
		}
	}
}
