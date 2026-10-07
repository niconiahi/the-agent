package vimtool

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	neovim "github.com/neovim/go-client/nvim"

	"github.com/niconiahi/the-agent/layout"
)

type lease_table struct {
	mutex   sync.Mutex
	holders map[string]string
}

var leases = &lease_table{holders: map[string]string{}}

func (table *lease_table) take(path string, agent string) (holder string, fresh bool) {
	table.mutex.Lock()
	defer table.mutex.Unlock()
	if holder, ok := table.holders[path]; ok {
		return holder, false
	}
	table.holders[path] = agent
	return agent, true
}

func (table *lease_table) give_back(path string, agent string) {
	table.mutex.Lock()
	defer table.mutex.Unlock()
	if table.holders[path] == agent {
		delete(table.holders, path)
	}
}

func (table *lease_table) drop(agent string) []string {
	table.mutex.Lock()
	defer table.mutex.Unlock()
	paths := []string{}
	for path, holder := range table.holders {
		if holder == agent {
			paths = append(paths, path)
			delete(table.holders, path)
		}
	}
	return paths
}

type target struct {
	Absolute string `msgpack:"absolute"`
	Relative string `msgpack:"relative"`
	Advice   string `msgpack:"advice"`
}

func lease(client *neovim.Nvim, agent string, path string) (func(), error) {
	if agent == "" {
		return func() {}, nil
	}
	var resolved target
	code := `local path = ...; return { absolute = vim.fn.fnamemodify(path, ":p"), relative = vim.fn.fnamemodify(path, ":."), advice = require("the-agent.buffer").ADVICE }`
	if error := client.ExecLua(code, &resolved, path); error != nil {
		return nil, error
	}
	holder, fresh := leases.take(resolved.Absolute, agent)
	if holder != agent {
		return nil, fmt.Errorf("%s is being edited by %s; %s", resolved.Relative, holder_name(holder), resolved.Advice)
	}
	return func() {
		if fresh {
			leases.give_back(resolved.Absolute, agent)
		}
	}, nil
}

func leased_change(client *neovim.Nvim, agent string, path string, function string, arguments ...any) (buffer_result, error) {
	give_back, error := lease(client, agent, path)
	if error != nil {
		return buffer_result{}, error
	}
	changed, error := call_buffer_function(client, function, arguments...)
	if error != nil {
		give_back()
	}
	return changed, error
}

func Lease(client *neovim.Nvim, agent string, path string) error {
	_, error := lease(client, agent, path)
	return error
}

func Release(client *neovim.Nvim, agent string) error {
	paths := leases.drop(agent)
	if len(paths) == 0 {
		return nil
	}
	_, error := call_buffer_function(client, "release", paths, agent)
	return error
}

func holder_name(agent string) string {
	marker := string(filepath.Separator) + filepath.Join(layout.FOLDER, layout.SESSIONS) + string(filepath.Separator)
	_, name, found := strings.Cut(agent, marker)
	if !found {
		return "agent " + agent
	}
	if strings.Contains(name, string(filepath.Separator)) {
		return "subagent " + filepath.ToSlash(name)
	}
	return "session " + name
}
