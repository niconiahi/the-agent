package nvim

import (
	"sync"

	"github.com/niconiahi/the-agent/orchestrator"
)

type routes struct {
	mutex     sync.Mutex
	listeners map[string]func(orchestrator.AgentEvent)
}

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
