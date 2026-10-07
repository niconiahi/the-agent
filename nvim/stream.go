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

// FLUSH_INTERVAL is how often streamed text is pushed to Neovim. Deltas that
// arrive in between are sent together in one RPC call.
const FLUSH_INTERVAL = 40 * time.Millisecond

// stream appends a turn's output to the end of a session buffer while the
// turn runs. Text is collected by append and pushed to Neovim by flush,
// which runs on a ticker, so the agent loop never waits on a round trip per
// delta.
//
// The buffer is modelled as the text of its lines joined by "\n" (the file
// minus its final newline), so the last buffer line can be a partial line
// that later text continues.
type stream struct {
	client *neovim.Nvim
	buffer neovim.Buffer

	// write serializes edits to the buffer and guards last and tail.
	write sync.Mutex
	last  int    // index of the last buffer line
	tail  string // the text of that line

	// state guards what has been appended but not yet flushed.
	state sync.Mutex
	// pending is text not yet in the buffer.
	pending string
	// lines is the index of the last line once pending is flushed.
	lines int
	// ends_line reports whether the text so far, pending included, ends
	// with "\n".
	ends_line bool

	stop chan struct{}
	done chan struct{}
}

// start_stream locks buffer, whose current contents are text (as written to
// disk), and starts streaming into it.
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

// append queues text to go at the end of the buffer.
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

// begin queues a separating blank line followed by line, and returns the
// index line will have in the buffer.
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

// flush pushes pending text to the buffer in one call.
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

// replace flushes, then replaces the buffer line at index with line.
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

// finish stops the ticker, flushes what is left, unlocks the buffer and
// saves it. The buffer is unlocked even when the flush fails.
func (current *stream) finish() error {
	close(current.stop)
	<-current.done
	flushed := current.flush()
	return errors.Join(flushed, current.client.ExecLua(`require("the-agent.stream").finish(...)`, nil, int(current.buffer)))
}

func (current *stream) set_lines(first int, last int, lines []string) error {
	return current.client.ExecLua(`require("the-agent.stream").set_lines(...)`, nil, int(current.buffer), first, last, lines)
}

// reply_writer turns the agent's events into assistant turns streamed into
// the buffer. It runs on the agent loop goroutine.
type reply_writer struct {
	output *stream
	model  string
	now    func() time.Time

	open    bool // an assistant turn has been started and not ended
	heading int  // buffer line of the open turn's heading
	at      time.Time
	// blocks reports whether the open turn has a block (text, thinking or
	// tool call) yet; the next one is separated from it by a blank line.
	blocks bool
	// in_text reports whether a text block is being streamed.
	in_text bool
	// held is trailing whitespace not written yet: it is only written once
	// more text follows, so a text block never ends in blank space.
	held string
	// wrote reports whether any assistant turn was written.
	wrote bool
	// failure collects errors rendering blocks.
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
			// Thinking is written whole: its fence length depends on its text.
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
			// Results go at the end of the turn that called the tool.
			writer.output.begin(session.ToolResultBlock(reply, writer.now()))
		}
	}
}

// open_turn starts an assistant turn with a provisional heading, completed
// by end once the reply's token count is known.
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

// start_block opens the turn if needed and separates a new block from the
// previous one.
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

// end completes the open turn's heading with how the reply ended.
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
