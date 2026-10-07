package nvim

import (
	"fmt"

	neovim "github.com/neovim/go-client/nvim"

	"github.com/niconiahi/the-agent/message"
	"github.com/niconiahi/the-agent/session"
)

// tokens is a session's estimated size against its ceiling.
type tokens struct {
	count   int
	ceiling int
}

// NEAR_CEILING is the fraction of the ceiling from which the statusline
// warns.
const NEAR_CEILING = 0.9

func (value tokens) above() bool { return value.count > value.ceiling }

func (value tokens) near() bool {
	return float64(value.count) >= NEAR_CEILING*float64(value.ceiling)
}

func (value tokens) String() string {
	return fmt.Sprintf("%s / %s tokens", session.FormatCount(value.count), session.FormatCount(value.ceiling))
}

// count measures what sending messages under prompt would cost, against the
// ceiling configured in the Lua plugin (setup{ ceiling = n }), clamped to
// the model's context window.
func (current *frontend) count(client *neovim.Nvim, prompt string, messages []message.Message) (tokens, error) {
	var configured int
	if error := client.ExecLua(`return require("the-agent").config.ceiling or 0`, &configured); error != nil {
		return tokens{}, error
	}
	return tokens{
		count:   session.EstimateTokens(prompt, messages),
		ceiling: session.Ceiling(configured, current.config.Model.ContextWindow),
	}, nil
}

// refresh measures the session in buffer and publishes the count for the
// statusline. The Lua plugin calls it (as a notification) when a session
// buffer is entered or changed.
func (current *frontend) refresh(client *neovim.Nvim, buffer int) error {
	handle := neovim.Buffer(buffer)
	text, error := buffer_text(client, handle)
	if error != nil {
		return error
	}
	parsed, error := session.Parse(text)
	if error != nil {
		return error
	}
	// Count what :TASend would send, timestamp included.
	parsed.StampLastUser(current.config.Now())
	dir, error := session_dir(client, handle)
	if error != nil {
		return error
	}
	// A missing system prompt is reported by :TASend; the count goes on.
	prompt, _ := system_prompt(parsed, dir)
	size, error := current.count(client, prompt, parsed.Messages())
	if error != nil {
		return error
	}
	return publish(client, handle, size)
}

// publish stores the count in b:the_agent_tokens, which the statusline
// renders, and redraws the statuslines.
func publish(client *neovim.Nvim, buffer neovim.Buffer, size tokens) error {
	batch := client.NewBatch()
	batch.SetBufferVar(buffer, "the_agent_tokens", map[string]any{
		"count":   size.count,
		"ceiling": size.ceiling,
		"near":    size.near(),
		"text":    size.String(),
	})
	batch.Command("redrawstatus!")
	return batch.Execute()
}

func ceiling_error(value tokens) error {
	return fmt.Errorf("session is at %s tokens, above the ceiling of %s: trim it and send again",
		session.FormatCount(value.count), session.FormatCount(value.ceiling))
}
