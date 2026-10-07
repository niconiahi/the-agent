package nvim

import (
	"context"
	"errors"
	"time"

	neovim "github.com/neovim/go-client/nvim"
)

const ABORT_WAIT = 5 * time.Second

type turn struct {
	context context.Context
	cancel  context.CancelFunc
	done    chan struct{}
}

func (current *frontend) start(buffer int) *turn {
	current.mutex.Lock()
	defer current.mutex.Unlock()
	if current.running[buffer] != nil {
		return nil
	}
	turn_context, cancel := context.WithCancel(context.Background())
	running := &turn{context: turn_context, cancel: cancel, done: make(chan struct{})}
	current.running[buffer] = running
	return running
}

func (current *frontend) finish(buffer int) {
	current.mutex.Lock()
	defer current.mutex.Unlock()
	if running := current.running[buffer]; running != nil {
		running.cancel()
		close(running.done)
		delete(current.running, buffer)
	}
}

func (current *frontend) abort(_ *neovim.Nvim, buffer int) error {
	current.mutex.Lock()
	running := current.running[buffer]
	current.mutex.Unlock()
	if running == nil {
		return errors.New("no turn is running in this session")
	}
	running.cancel()
	select {
	case <-running.done:
	case <-time.After(ABORT_WAIT):
	}
	return nil
}
