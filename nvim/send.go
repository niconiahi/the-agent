package nvim

import (
	"context"
	"errors"
	"fmt"
	"strings"

	neovim "github.com/neovim/go-client/nvim"

	"github.com/niconiahi/the-agent/message"
	"github.com/niconiahi/the-agent/orchestrator"
	"github.com/niconiahi/the-agent/vimtool"
)

const LOG_LEVEL_ERROR = 4

var ERROR_NOTHING_TO_SEND = errors.New("nothing to send: write your message under a ## user heading at the bottom")

func (current *frontend) send(client *neovim.Nvim, buffer int) error {
	if current.config.Ready != nil {
		if error := current.config.Ready(); error != nil {
			return error
		}
	}

	handle := neovim.Buffer(buffer)
	prepared, error := current.prepare(client, handle)
	if error != nil {
		return error
	}
	if _, ok := prepared.last(); !ok {
		return ERROR_NOTHING_TO_SEND
	}
	if prepared.missing != nil {
		return prepared.missing
	}
	if prepared.size.above() {
		publish(client, handle, prepared.size)
		return ceiling_error(prepared.size)
	}
	text, parsed := prepared.text, prepared.parsed

	running := current.start(buffer)
	if running == nil {
		return errors.New("a turn is already running in this session")
	}

	stamped := parsed.Render()
	parsed.Repair()
	repaired := parsed.Render()

	output, error := start_stream(client, handle, text, stamped)
	if error != nil {
		current.finish(buffer)
		return error
	}
	go func() {
		defer current.finish(buffer)
		replies := &reply_writer{output: output, directory: prepared.directory, model: current.config.Model.ID, now: current.config.Now}
		error := current.run(running.context, prepared, replies.handle)
		if running.context.Err() != nil {
			error = nil
		}
		error = errors.Join(error, replies.failure)

		if replies.wrote {
			output.begin("## user\n")
		}
		if finish_error := output.finish(); finish_error != nil {
			error = errors.Join(error, finish_error)
		}
		if repair_error := repair_buffer(client, handle, stamped, repaired); repair_error != nil {
			error = errors.Join(error, repair_error)
		}
		if error != nil {
			notify(client, error.Error(), LOG_LEVEL_ERROR)
		}
		current.refresh(client, buffer)
	}()
	return nil
}

func (current *frontend) run(invocation_context context.Context, prepared *request, listener func(orchestrator.AgentEvent)) error {
	history := prepared.history()
	last, _ := prepared.last()
	agent := orchestrator.New(
		orchestrator.WithModel(current.config.Model),
		orchestrator.WithTools(current.config.Tools),
		orchestrator.WithSystemPrompt(prepared.prompt),
		orchestrator.WithStreamOptions(current.config.StreamOptions),
		orchestrator.WithTransformContext(func(_ context.Context, messages []message.Message) []message.Message {
			return append(append([]message.Message{}, history...), messages...)
		}),
	)

	agent.Subscribe(listener)
	return agent.Prompt(vimtool.WithSession(invocation_context, prepared.directory, current.config.Now), last)
}

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

func replace_changed_line(batch *neovim.Batch, buffer neovim.Buffer, before string, after string) error {
	if before == after {
		return nil
	}
	old_lines, new_lines := to_lines(before), to_lines(after)
	if len(old_lines) != len(new_lines) {
		return fmt.Errorf("internal error: stamping changed the line count")
	}
	for index := range old_lines {
		if string(old_lines[index]) != string(new_lines[index]) {
			batch.SetBufferLines(buffer, index, index+1, true, new_lines[index:index+1])
			return nil
		}
	}
	return nil
}

func notify(client *neovim.Nvim, text string, level int) {
	client.ExecLua(`require("the-agent").notify(...)`, nil, text, level)
}
