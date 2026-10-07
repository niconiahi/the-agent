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

func ceiling_error(value tokens) error {
	return fmt.Errorf("session is at %s tokens, above the ceiling of %s: trim it and send again",
		session.FormatCount(value.count), session.FormatCount(value.ceiling))
}
