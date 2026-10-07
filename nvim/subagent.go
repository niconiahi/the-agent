package nvim

import (
	"errors"
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

type amendment struct {
	parent subagent.Parent
	report string
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

func (current *frontend) begin(client *neovim.Nvim, directory string) error {
	if error := vimtool.Lease(client, directory, filepath.Join(directory, "session.md")); error != nil {
		return error
	}
	current.mutex.Lock()
	defer current.mutex.Unlock()
	current.active[directory] = []amendment{}
	return nil
}

func (current *frontend) settle(client *neovim.Nvim, directory string) error {
	current.mutex.Lock()
	pending := current.active[directory]
	delete(current.active, directory)
	current.mutex.Unlock()
	failures := []error{}
	for _, deferred := range pending {
		failures = append(failures, current.apply(client, deferred))
	}
	return errors.Join(failures...)
}

func (current *frontend) amend(client *neovim.Nvim, directory string, report string) error {
	if report == "" {
		return nil
	}
	parent, ok := subagent.ParentOf(directory)
	if !ok {
		return nil
	}
	next := amendment{parent: parent, report: report}
	current.mutex.Lock()
	pending, running := current.active[filepath.Dir(parent.Path)]
	if running {
		current.active[filepath.Dir(parent.Path)] = append(pending, next)
	}
	current.mutex.Unlock()
	if running {
		return nil
	}
	return current.apply(client, next)
}

func (current *frontend) apply(client *neovim.Nvim, next amendment) error {
	buffer, text, error := session_text(client, next.parent.Path)
	if error != nil {
		return error
	}
	amended, ok := session.Amend(text, next.parent.Origin, next.report, current.config.Now())
	if !ok {
		return nil
	}
	if buffer < 0 {
		return os.WriteFile(next.parent.Path, []byte(amended), 0o644)
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
	if error := current.frontend.begin(current.client, directory); error != nil {
		return nil, nil, error
	}
	output, error := start_stream(current.client, buffer, text, text)
	if error != nil {
		return nil, nil, errors.Join(error, vimtool.Release(current.client, directory), current.frontend.settle(current.client, directory))
	}
	writer := current.frontend.start_writer(current.client, output, directory)
	remove := current.frontend.routes.add(directory, writer.handle)
	return current.frontend.routes.route, current.end(directory, writer, remove), nil
}

func (current host) end(directory string, writer *session_writer, remove func()) func(error) {
	return func(error) {
		remove()
		failure := errors.Join(vimtool.Release(current.client, directory), writer.finish(), current.frontend.settle(current.client, directory))
		if failure != nil {
			notify(current.client, failure.Error(), LOG_LEVEL_ERROR)
		}
	}
}
