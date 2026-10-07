package subagent

import (
	"fmt"
	"slices"
	"strings"

	"github.com/niconiahi/the-agent/tool"
)

type role string

const (
	ROLE_ROOT     role = "root"
	ROLE_EXPLORER role = "explorer"
	ROLE_WORKER   role = "worker"
)

const DEFAULT_MAX_DEPTH = 3

var WORKER_TOOLS = []string{"edit", "write", "filter", "bash_write"}

func (current role) delegates() []role {
	if current == ROLE_EXPLORER {
		return []role{ROLE_EXPLORER}
	}
	return []role{ROLE_EXPLORER, ROLE_WORKER}
}

func (current role) child(value string) (role, error) {
	if value == "" {
		return ROLE_EXPLORER, nil
	}
	delegates := current.delegates()
	if slices.Contains(delegates, role(value)) {
		return role(value), nil
	}
	names := []string{}
	for _, delegate := range delegates {
		names = append(names, string(delegate))
	}
	return "", fmt.Errorf("unknown role %q: use %s", value, strings.Join(names, " or "))
}

func (config Config) explorer_tools() []string {
	if config.ExplorerTools != nil {
		return config.ExplorerTools
	}
	return []string{"read", "grep", "find", "ls", "bash_read"}
}

func (config Config) max_depth() int {
	if config.MaxDepth > 0 {
		return config.MaxDepth
	}
	return DEFAULT_MAX_DEPTH
}

func (config Config) tools(current role, depth int) []tool.Tool {
	var tools []tool.Tool
	switch current {
	case ROLE_ROOT:
		tools = slices.Clone(config.Tools)
	case ROLE_WORKER:
		tools = pick(config.Tools, append(slices.Clone(config.explorer_tools()), WORKER_TOOLS...))
	default:
		tools = pick(config.Tools, config.explorer_tools())
	}
	if depth < config.max_depth() {
		tools = append(tools, config.task(current))
	}
	return tools
}

func pick(tools []tool.Tool, names []string) []tool.Tool {
	picked := []tool.Tool{}
	for _, current := range tools {
		if slices.Contains(names, current.Name) {
			picked = append(picked, current)
		}
	}
	return picked
}
