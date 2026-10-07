package nvim

import (
	"fmt"

	neovim "github.com/neovim/go-client/nvim"

	"github.com/niconiahi/the-agent/message"
	"github.com/niconiahi/the-agent/session"
)

type tokens struct {
	count   int
	ceiling int
}

const NEAR_CEILING = 0.9

func (value tokens) above() bool { return value.count > value.ceiling }

func (value tokens) near() bool {
	return float64(value.count) >= NEAR_CEILING*float64(value.ceiling)
}

func (value tokens) String() string {
	return fmt.Sprintf("%s / %s tokens", session.FormatCount(value.count), session.FormatCount(value.ceiling))
}

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

	parsed.StampLastUser(current.config.Now())
	directory, error := session_dir(client, handle)
	if error != nil {
		return error
	}

	prompt, _ := system_prompt(parsed, directory)
	messages := parsed.Messages()
	if loaded, error := session.LoadImages(messages, directory); error == nil {
		messages = loaded
	}
	size, error := current.count(client, prompt, messages)
	if error != nil {
		return error
	}
	return publish(client, handle, size)
}

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
