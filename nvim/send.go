package nvim

import (
	"context"
	"errors"
	"fmt"
	"strings"

	neovim "github.com/neovim/go-client/nvim"

	"github.com/niconiahi/the-agent/message"
	"github.com/niconiahi/the-agent/orchestrator"
	"github.com/niconiahi/the-agent/session"
)

const LOG_LEVEL_ERROR = 4 // vim.log.levels.ERROR

// send parses the buffer, stamps the last user heading and starts the turn.
// It returns as soon as the turn is started; the reply is appended to the
// buffer and saved when the turn ends.
func (current *frontend) send(client *neovim.Nvim, buffer int) error {
	if current.config.Ready != nil {
		if error := current.config.Ready(); error != nil {
			return error
		}
	}

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

	messages := parsed.Messages()
	if len(messages) == 0 {
		return errors.New("nothing to send: write your message under a ## user heading at the bottom")
	}
	last, ok := messages[len(messages)-1].(message.UserMessage)
	if !ok {
		return errors.New("nothing to send: write your message under a ## user heading at the bottom")
	}

	if !current.start(buffer) {
		return errors.New("a turn is already running in this session")
	}

	stamped := parsed.Render()
	if error := replace_changed_line(client, handle, text, stamped); error != nil {
		current.finish(buffer)
		return error
	}

	go func() {
		defer current.finish(buffer)
		replies, error := current.run(messages[:len(messages)-1], last)

		for _, reply := range replies {
			switch typed := reply.(type) {
			case *message.AssistantMessage:
				if render_error := parsed.AppendAssistantMessage(current.config.Model.ID, current.config.Now(), *typed); render_error != nil {
					error = errors.Join(error, render_error)
				}
			case message.ToolResultMessage:
				parsed.AppendToolResult(typed, current.config.Now())
			}
		}
		if len(replies) > 0 {
			parsed.AppendUser()
			if append_error := append_tail(client, handle, stamped, parsed.Render()); append_error != nil {
				error = errors.Join(error, append_error)
			}
		}
		if error != nil {
			notify(client, error.Error(), LOG_LEVEL_ERROR)
		}
	}()
	return nil
}

// run sends history plus last to the model and returns, in order, the
// assistant replies that have content (*message.AssistantMessage) and the
// results of the tools they called (message.ToolResultMessage).
func (current *frontend) run(history []message.Message, last message.UserMessage) ([]message.Message, error) {
	agent := orchestrator.New(
		orchestrator.WithModel(current.config.Model),
		orchestrator.WithTools(current.config.Tools),
		orchestrator.WithSystemPrompt(current.config.SystemPrompt),
		orchestrator.WithStreamOptions(current.config.StreamOptions),
		// The agent only holds this send's messages; the rest of the session
		// is prepended on every request.
		orchestrator.WithTransformContext(func(_ context.Context, messages []message.Message) []message.Message {
			return append(append([]message.Message{}, history...), messages...)
		}),
	)

	replies := []message.Message{}
	agent.Subscribe(func(event orchestrator.AgentEvent) {
		end, ok := event.(orchestrator.MessageEndEvent)
		if !ok {
			return
		}
		switch typed := end.Message.(type) {
		case *message.AssistantMessage:
			if len(typed.Content) > 0 {
				replies = append(replies, typed)
			}
		case message.ToolResultMessage:
			replies = append(replies, typed)
		}
	})

	error := agent.Prompt(context.Background(), last)
	return replies, error
}

func (current *frontend) start(buffer int) bool {
	current.mutex.Lock()
	defer current.mutex.Unlock()
	if current.running[buffer] {
		return false
	}
	current.running[buffer] = true
	return true
}

func (current *frontend) finish(buffer int) {
	current.mutex.Lock()
	defer current.mutex.Unlock()
	delete(current.running, buffer)
}

// buffer_text is the buffer as it would be written to disk.
func buffer_text(client *neovim.Nvim, buffer neovim.Buffer) (string, error) {
	lines, error := client.BufferLines(buffer, 0, -1, true)
	if error != nil {
		return "", error
	}
	joined := make([]string, len(lines))
	for index, line := range lines {
		joined[index] = string(line)
	}
	return strings.Join(joined, "\n") + "\n", nil
}

func to_lines(text string) [][]byte {
	lines := [][]byte{}
	for _, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		lines = append(lines, []byte(line))
	}
	return lines
}

// replace_changed_line writes the single line that differs between before
// and after (they have the same number of lines), leaving the rest of the
// buffer, its cursor and its undo history alone.
func replace_changed_line(client *neovim.Nvim, buffer neovim.Buffer, before string, after string) error {
	if before == after {
		return nil
	}
	old_lines, new_lines := to_lines(before), to_lines(after)
	if len(old_lines) != len(new_lines) {
		return fmt.Errorf("internal error: stamping changed the line count")
	}
	for index := range old_lines {
		if string(old_lines[index]) != string(new_lines[index]) {
			return client.SetBufferLines(buffer, index, index+1, true, new_lines[index:index+1])
		}
	}
	return nil
}

// append_tail appends what after adds past before to the end of the buffer
// and saves it.
func append_tail(client *neovim.Nvim, buffer neovim.Buffer, before string, after string) error {
	tail, ok := strings.CutPrefix(after, before)
	if !ok {
		return fmt.Errorf("internal error: rendered session does not extend the buffer")
	}
	if tail == "" {
		return nil
	}
	batch := client.NewBatch()
	batch.SetBufferLines(buffer, -1, -1, true, to_lines(tail))
	batch.ExecLua(`require("the-agent").fold_thinking(...)`, nil, int(buffer), len(to_lines(before))+1)
	batch.ExecLua(`vim.api.nvim_buf_call(..., function() vim.cmd("silent write") end)`, nil, int(buffer))
	return batch.Execute()
}

func notify(client *neovim.Nvim, text string, level int) {
	client.ExecLua(`require("the-agent").notify(...)`, nil, text, level)
}
