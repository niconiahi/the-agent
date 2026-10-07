package nvim

import (
	"sync"

	"github.com/niconiahi/the-agent/orchestrator"
)

// routes sends each orchestrator event to the stream of the agent that
// emitted it. An agent's ID is its session directory, the same identity
// vimtool keys its per-agent state by, so every running agent (a root
// session, or later a subagent whose events reach us through its parent)
// writes only into its own session.md. Events of an agent with no route are
// dropped.
type routes struct {
	mutex     sync.Mutex
	listeners map[string]func(orchestrator.AgentEvent)
}

// add routes the events of agent to listener until the returned func is
// called.
func (current *routes) add(agent string, listener func(orchestrator.AgentEvent)) func() {
	current.mutex.Lock()
	defer current.mutex.Unlock()
	if current.listeners == nil {
		current.listeners = map[string]func(orchestrator.AgentEvent){}
	}
	current.listeners[agent] = listener
	return func() {
		current.mutex.Lock()
		defer current.mutex.Unlock()
		delete(current.listeners, agent)
	}
}

func (current *routes) route(event orchestrator.AgentEvent) {
	current.mutex.Lock()
	listener := current.listeners[event.AgentID()]
	current.mutex.Unlock()
	if listener != nil {
		listener(event)
	}
}
