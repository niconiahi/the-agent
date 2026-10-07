package nvim

import (
	"errors"
	"strings"
	"sync"
	"time"
	"unicode"

	neovim "github.com/neovim/go-client/nvim"

	"github.com/niconiahi/the-agent/message"
	"github.com/niconiahi/the-agent/orchestrator"
	"github.com/niconiahi/the-agent/sender"
	"github.com/niconiahi/the-agent/session"
)

const FLUSH_INTERVAL = 40 * time.Millisecond

type stream struct {
	client *neovim.Nvim
	buffer neovim.Buffer

	write sync.Mutex
	last  int
	tail  string

	state sync.Mutex

	pending string

	lines int

	ends_line bool

	stop chan struct{}
	done chan struct{}
}

func start_stream(client *neovim.Nvim, buffer neovim.Buffer, text string) (*stream, error) {
	if error := client.ExecLua(`require("the-agent.stream").lock(...)`, nil, int(buffer)); error != nil {
		return nil, error
	}
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	current := &stream{
		client:    client,
		buffer:    buffer,
		last:      len(lines) - 1,
		tail:      lines[len(lines)-1],
		lines:     len(lines) - 1,
		ends_line: lines[len(lines)-1] == "" && len(lines) > 1,
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
	}
	go current.tick()
	return current, nil
}

func (current *stream) tick() {
	defer close(current.done)
	ticker := time.NewTicker(FLUSH_INTERVAL)
	defer ticker.Stop()
	for {
		select {
		case <-current.stop:
			return
		case <-ticker.C:
			current.flush()
		}
	}
}

func (current *stream) append(text string) {
	current.state.Lock()
	defer current.state.Unlock()
	current.append_locked(text)
}

func (current *stream) append_locked(text string) {
	if text == "" {
		return
	}
	current.pending += text
	current.lines += strings.Count(text, "\n")
	current.ends_line = strings.HasSuffix(text, "\n")
}

func (current *stream) begin(line string) int {
	current.state.Lock()
	defer current.state.Unlock()
	if current.ends_line {
		current.append_locked("\n")
	} else {
		current.append_locked("\n\n")
	}
	index := current.lines
	current.append_locked(line)
	return index
}

func (current *stream) flush() error {
	current.write.Lock()
	defer current.write.Unlock()

	current.state.Lock()
	pending := current.pending
	current.pending = ""
	current.state.Unlock()
	if pending == "" {
		return nil
	}

	lines := strings.Split(current.tail+pending, "\n")
	error := current.set_lines(current.last, current.last+1, lines)
	current.last += len(lines) - 1
	current.tail = lines[len(lines)-1]
	return error
}

func (current *stream) replace(index int, line string) error {
	if error := current.flush(); error != nil {
		return error
	}
	current.write.Lock()
	defer current.write.Unlock()
	if index == current.last {
		current.tail = line
	}
	return current.set_lines(index, index+1, []string{line})
}

func (current *stream) finish() error {
	close(current.stop)
	<-current.done
	flushed := current.flush()
	return errors.Join(flushed, current.client.ExecLua(`require("the-agent.stream").finish(...)`, nil, int(current.buffer)))
}

func (current *stream) set_lines(first int, last int, lines []string) error {
	return current.client.ExecLua(`require("the-agent.stream").set_lines(...)`, nil, int(current.buffer), first, last, lines)
}

type reply_writer struct {
	output *stream
	model  string
	now    func() time.Time

	open    bool
	heading int
	at      time.Time

	blocks bool

	in_text bool

	held string

	wrote bool

	failure error
}

func (writer *reply_writer) handle(event orchestrator.AgentEvent) {
	switch typed := event.(type) {
	case orchestrator.MessageUpdateEvent:
		switch update := typed.SenderEvent.(type) {
		case sender.EventTextDelta:
			writer.text(update.Delta)
		case sender.EventTextEnd:
			writer.end_text()
		case sender.EventThinkingEnd:

			if strings.TrimSpace(update.FullText) != "" {
				writer.block(session.ThinkingBlock(update.FullText))
			}
		case sender.EventToolCallEnd:
			writer.open_turn()
			block, error := session.ToolCallBlock(update.ToolCall, writer.at)
			if error != nil {
				writer.failure = errors.Join(writer.failure, error)
				return
			}
			writer.block(block)
		}
	case orchestrator.MessageEndEvent:
		switch reply := typed.Message.(type) {
		case *message.AssistantMessage:
			writer.end(reply)
		case message.ToolResultMessage:

			writer.output.begin(session.ToolResultBlock(reply, writer.now()))
		}
	}
}

func (writer *reply_writer) open_turn() {
	if writer.open {
		return
	}
	writer.open = true
	writer.wrote = true
	writer.blocks = false
	writer.at = writer.now()
	writer.heading = writer.output.begin(writer.heading_line(""))
	writer.output.append("\n")
}

func (writer *reply_writer) start_block() {
	writer.end_text()
	writer.open_turn()
	writer.output.append("\n")
	if writer.blocks {
		writer.output.append("\n")
	}
	writer.blocks = true
}

func (writer *reply_writer) block(text string) {
	writer.start_block()
	writer.output.append(text)
}

func (writer *reply_writer) text(delta string) {
	if !writer.in_text {
		delta = strings.TrimLeftFunc(delta, unicode.IsSpace)
		if delta == "" {
			return
		}
		writer.start_block()
		writer.in_text = true
	}
	text := writer.held + delta
	trimmed := strings.TrimRightFunc(text, unicode.IsSpace)
	writer.held = text[len(trimmed):]
	writer.output.append(trimmed)
}

func (writer *reply_writer) end_text() {
	writer.in_text = false
	writer.held = ""
}

func (writer *reply_writer) end(reply *message.AssistantMessage) {
	writer.end_text()
	if !writer.open {
		return
	}
	writer.open = false
	outcome := session.FormatTokens(reply.Usage.TotalTokens)
	switch reply.StopReason {
	case message.STOP_REASON_ABORTED, message.STOP_REASON_ERROR:
		outcome = string(reply.StopReason)
	}
	writer.output.replace(writer.heading, writer.heading_line(outcome))
}

func (writer *reply_writer) heading_line(outcome string) string {
	line := "## assistant · " + writer.model + " · " + writer.at.UTC().Format(session.TIMESTAMP_FORMAT)
	if outcome != "" {
		line += " · " + outcome
	}
	return line
}
