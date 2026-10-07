package nvim

import (
	"context"
	"errors"
	"time"

	neovim "github.com/neovim/go-client/nvim"
)

// ABORT_WAIT is how long :TAAbort waits for the turn to wind down and
// unlock its buffer before returning anyway.
const ABORT_WAIT = 5 * time.Second

// turn is a running :TASend.
type turn struct {
	context context.Context
	cancel  context.CancelFunc
	done    chan struct{} // closed once the buffer is unlocked and saved
}

// start reserves buffer for a new turn; it returns nil when one is already
// running there.
func (current *frontend) start(buffer int) *turn {
	current.mutex.Lock()
	defer current.mutex.Unlock()
	if current.running[buffer] != nil {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	running := &turn{context: ctx, cancel: cancel, done: make(chan struct{})}
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

// abort cancels the turn running in buffer and waits for it to keep what
// was streamed, unlock the buffer and save it.
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
