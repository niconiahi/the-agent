package nvim

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	neovim "github.com/neovim/go-client/nvim"

	"github.com/niconiahi/the-agent/orchestrator"
	"github.com/niconiahi/the-agent/session"
	"github.com/niconiahi/the-agent/subagent"
	"github.com/niconiahi/the-agent/tool"
	"github.com/niconiahi/the-agent/vimtool"
)

type host struct {
	frontend *frontend
	client   *neovim.Nvim
}

func (current *frontend) tools(client *neovim.Nvim, directory string) []tool.Tool {
	return subagent.Config{
		Model:         current.config.Model,
		StreamOptions: current.config.StreamOptions,
		Tools:         current.config.Tools,
		ExplorerTools: current.config.ExplorerTools,
		MaxDepth:      current.config.MaxDepth,
		SystemPrompt:  SystemPromptPath(current.config.Project),
		Host:          host{frontend: current, client: client},
		Now:           current.config.Now,
	}.ToolsFor(directory)
}

func (current *frontend) amend(client *neovim.Nvim, directory string, report string) error {
	if report == "" {
		return nil
	}
	parent, ok := subagent.ParentOf(directory)
	if !ok {
		return nil
	}
	buffer, text, error := session_text(client, parent.Path)
	if error != nil {
		return error
	}
	amended, ok := session.Amend(text, parent.Origin, report, current.config.Now())
	if !ok {
		return nil
	}
	if buffer < 0 {
		return os.WriteFile(parent.Path, []byte(amended), 0o644)
	}
	var modifiable bool
	if error := client.BufferOption(neovim.Buffer(buffer), "modifiable", &modifiable); error != nil {
		return error
	}
	if !modifiable {
		return fmt.Errorf("%s is running: its result for %s was not amended", parent.Path, filepath.Base(directory))
	}
	return repair_buffer(client, neovim.Buffer(buffer), text, amended)
}

func session_text(client *neovim.Nvim, path string) (int, string, error) {
	var buffer int
	if error := client.ExecLua(`
		local buffer = vim.fn.bufnr(...)
		if buffer < 0 or not vim.api.nvim_buf_is_loaded(buffer) then return -1 end
		return buffer
	`, &buffer, path); error != nil {
		return 0, "", error
	}
	if buffer >= 0 {
		text, error := buffer_text(client, neovim.Buffer(buffer))
		return buffer, text, error
	}
	contents, error := os.ReadFile(path)
	return buffer, string(contents), error
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
	directory := filepath.Dir(path)
	if error := vimtool.Lease(current.client, directory, path); error != nil {
		return nil, nil, error
	}
	output, error := start_stream(current.client, buffer, text, text)
	if error != nil {
		return nil, nil, errors.Join(error, vimtool.Release(current.client, directory))
	}
	writer := current.frontend.start_writer(current.client, output, directory)
	remove := current.frontend.routes.add(directory, writer.handle)
	return current.frontend.routes.route, current.end(directory, writer, remove), nil
}

// end finishes the child's file and releases the child's write leases: its
// task is over.
func (current host) end(directory string, writer *session_writer, remove func()) func(error) {
	return func(error) {
		remove()
		failure := errors.Join(vimtool.Release(current.client, directory), writer.finish())
		if failure != nil {
			notify(current.client, failure.Error(), LOG_LEVEL_ERROR)
		}
	}
}
