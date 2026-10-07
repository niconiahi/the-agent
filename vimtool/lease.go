package vimtool

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	neovim "github.com/neovim/go-client/nvim"

	"github.com/niconiahi/the-agent/layout"
)

const LEASE_ADVICE = "work on other files or finish without it"

// The write leases of every agent in this process: which agent, by session
// directory, holds each file, by absolute path. An agent takes a file's
// lease at its first change and keeps it until its task ends.
type lease_table struct {
	mutex   sync.Mutex
	holders map[string]string
}

var leases = &lease_table{holders: map[string]string{}}

// take gives agent the lease on path, unless another agent holds it, whom
// it returns. fresh reports that agent did not hold it before.
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

// drop ends every lease agent holds and returns their paths.
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
}

// lease takes the lease on path for agent, resolved the way the buffer
// functions resolve it, against Neovim's working directory. It fails at
// once, never waiting, when another agent holds it. The returned function
// gives a lease taken by this call back, for a change that then failed.
// Outside a session there is no agent and nothing is leased.
func lease(client *neovim.Nvim, agent string, path string) (func(), error) {
	if agent == "" {
		return func() {}, nil
	}
	var resolved target
	code := `local path = ...; return { absolute = vim.fn.fnamemodify(path, ":p"), relative = vim.fn.fnamemodify(path, ":.") }`
	if error := client.ExecLua(code, &resolved, path); error != nil {
		return nil, error
	}
	holder, fresh := leases.take(resolved.Absolute, agent)
	if holder != agent {
		return nil, fmt.Errorf("%s is being edited by %s; %s", resolved.Relative, holder_name(holder), LEASE_ADVICE)
	}
	return func() {
		if fresh {
			leases.give_back(resolved.Absolute, agent)
		}
	}, nil
}

// Lease takes the lease on path for the agent whose session directory is
// agent, as its first change would. The frontend leases a running
// session's own session.md this way, so no other agent can change it.
func Lease(client *neovim.Nvim, agent string, path string) error {
	_, error := lease(client, agent, path)
	return error
}

// Release ends every lease the agent holds, when its task ends, and makes
// the buffers its leases locked modifiable again, all in one request.
func Release(client *neovim.Nvim, agent string) error {
	paths := leases.drop(agent)
	if len(paths) == 0 {
		return nil
	}
	_, error := call_buffer_function(client, "release", paths, agent)
	return error
}

// holder_name names an agent by its session directory: "session foo" for
// a root session, "subagent foo/01-map-callers" for a child.
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
