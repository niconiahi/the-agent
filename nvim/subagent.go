package nvim

import (
	"fmt"
	"os"
	"path/filepath"

	neovim "github.com/neovim/go-client/nvim"

	"github.com/niconiahi/the-agent/orchestrator"
	"github.com/niconiahi/the-agent/session"
	"github.com/niconiahi/the-agent/subagent"
	"github.com/niconiahi/the-agent/tool"
)

type host struct {
	frontend *frontend
	client   *neovim.Nvim
}

// tools is the tool set of the session in directory: everything for a
// root session, its role's tools for a subagent, and task below the depth
// limit.
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

// amend puts a continued subagent's new report into its direct parent's
// tool result, in the parent's buffer when it is loaded and on disk
// otherwise. A parent whose result is gone is left alone.
func (current *frontend) amend(client *neovim.Nvim, directory string, report string) error {
	if subagent.Depth(directory) == 0 || report == "" {
		return nil
	}
	path := filepath.Join(filepath.Dir(directory), "session.md")
	link := subagent.Link{Folder: filepath.Base(directory)}.String()
	var number int
	if error := client.ExecLua(`
		local buffer = vim.fn.bufnr(...)
		if buffer < 0 or not vim.api.nvim_buf_is_loaded(buffer) then return -1 end
		return buffer
	`, &number, path); error != nil {
		return error
	}
	if number < 0 {
		contents, error := os.ReadFile(path)
		if error != nil {
			return error
		}
		if amended, ok := session.Amend(string(contents), link, report, current.config.Now()); ok {
			return os.WriteFile(path, []byte(amended), 0o644)
		}
		return nil
	}
	buffer := neovim.Buffer(number)
	text, error := buffer_text(client, buffer)
	if error != nil {
		return error
	}
	amended, ok := session.Amend(text, link, report, current.config.Now())
	if !ok {
		return nil
	}
	var modifiable bool
	if error := client.BufferOption(buffer, "modifiable", &modifiable); error != nil {
		return error
	}
	if !modifiable {
		return fmt.Errorf("%s is running: its result for %s was not amended", path, filepath.Base(directory))
	}
	return repair_buffer(client, buffer, text, amended)
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
