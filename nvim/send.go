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
		writer := current.start_writer(client, output, prepared.directory)
		error := current.run(running.context, client, prepared, writer.handle)
		if running.context.Err() != nil {
			error = nil
		}
		error = errors.Join(error, writer.finish())
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

type session_writer struct {
	replies  *reply_writer
	previews *previewer
}

func (current *frontend) start_writer(client *neovim.Nvim, output *stream, directory string) *session_writer {
	return &session_writer{
		replies:  &reply_writer{output: output, directory: directory, model: current.config.Model.ID, now: current.config.Now},
		previews: start_previewer(client),
	}
}

func (writer *session_writer) handle(event orchestrator.AgentEvent) {
	writer.replies.handle(event)
	writer.previews.handle(event)
}

func (writer *session_writer) finish() error {
	failure := errors.Join(writer.replies.failure, writer.previews.finish())
	if writer.replies.wrote {
		writer.replies.output.begin("## user\n")
	}
	return errors.Join(failure, writer.replies.output.finish())
}

func (current *frontend) run(invocation_context context.Context, client *neovim.Nvim, prepared *request, listener func(orchestrator.AgentEvent)) error {
	history := prepared.history()
	last, _ := prepared.last()
	agent := orchestrator.New(
		orchestrator.WithID(prepared.directory),
		orchestrator.WithModel(current.config.Model),
		orchestrator.WithTools(current.tools(client)),
		orchestrator.WithSystemPrompt(prepared.prompt),
		orchestrator.WithStreamOptions(current.config.StreamOptions),
		orchestrator.WithTransformContext(func(_ context.Context, messages []message.Message) []message.Message {
			return append(append([]message.Message{}, history...), messages...)
		}),
	)

	defer current.routes.add(agent.ID(), listener)()
	agent.Subscribe(current.routes.route)
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
