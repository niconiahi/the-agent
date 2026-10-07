package nvim

import (
	"encoding/json"
	"errors"
	"maps"
	"sync"
	"time"

	neovim "github.com/neovim/go-client/nvim"

	"github.com/niconiahi/the-agent/message"
	"github.com/niconiahi/the-agent/orchestrator"
	"github.com/niconiahi/the-agent/partialjson"
	"github.com/niconiahi/the-agent/sender"
)

const PREVIEW_TOOL = "edit"

type previewer struct {
	client *neovim.Nvim

	mutex     sync.Mutex
	streaming map[int]*edit_preview
	previews  map[string]*edit_preview
	failure   error

	stop chan struct{}
	done chan struct{}
}

type edit_preview struct {
	id        string
	agent     string
	arguments string
	shown     map[string]string
}

func start_previewer(client *neovim.Nvim) *previewer {
	current := &previewer{
		client:    client,
		streaming: map[int]*edit_preview{},
		previews:  map[string]*edit_preview{},
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
	}
	go current.tick()
	return current
}

func (current *previewer) tick() {
	defer close(current.done)
	ticker := time.NewTicker(FLUSH_INTERVAL)
	defer ticker.Stop()
	for {
		select {
		case <-current.stop:
			return
		case <-ticker.C:
			current.mutex.Lock()
			current.flush(current.previews)
			current.mutex.Unlock()
		}
	}
}

func (current *previewer) handle(event orchestrator.AgentEvent) {
	current.mutex.Lock()
	defer current.mutex.Unlock()
	switch typed := event.(type) {
	case orchestrator.MessageUpdateEvent:
		switch update := typed.SenderEvent.(type) {
		case sender.EventToolCallStart:
			if update.Name != PREVIEW_TOOL {
				return
			}
			preview := &edit_preview{id: update.ID, agent: typed.AgentID(), shown: map[string]string{}}
			current.streaming[update.ContentIndex] = preview
			current.previews[update.ID] = preview
		case sender.EventToolCallDelta:
			if preview := current.streaming[update.ContentIndex]; preview != nil {
				preview.arguments += update.Delta
			}
		case sender.EventToolCallEnd:
			preview := current.streaming[update.ContentIndex]
			if preview == nil {
				return
			}
			delete(current.streaming, update.ContentIndex)
			if preview.arguments == "" {
				arguments, _ := json.Marshal(update.ToolCall.Arguments)
				preview.arguments = string(arguments)
			}
			current.flush(map[string]*edit_preview{preview.id: preview})
		}
	case orchestrator.MessageEndEvent:
		if reply, ok := typed.Message.(*message.AssistantMessage); ok {
			current.streaming = map[int]*edit_preview{}
			if reply.StopReason == message.STOP_REASON_ERROR || reply.StopReason == message.STOP_REASON_ABORTED {
				current.clear(current.previews)
			}
		}
	case orchestrator.ToolExecutionEndEvent:
		if preview := current.previews[typed.ToolCallID]; preview != nil {
			current.clear(map[string]*edit_preview{preview.id: preview})
		}
	}
}

func fields(arguments string) map[string]string {
	read := partialjson.Read(arguments)
	shown := map[string]string{}
	for _, key := range []string{"path", "old_text", "new_text"} {
		if value, ok := read.Complete[key]; ok {
			shown[key] = value
		}
	}
	if read.Streaming == "new_text" {
		shown["new_text"] = read.Partial
	}
	return shown
}

func (current *previewer) flush(previews map[string]*edit_preview) {
	batch := current.client.NewBatch()
	changed := false
	for _, preview := range previews {
		shown := fields(preview.arguments)
		if maps.Equal(shown, preview.shown) {
			continue
		}
		preview.shown = shown
		changed = true
		batch.ExecLua(`require("the-agent.follow").preview(...)`, nil, preview.id, preview.agent, shown)
	}
	if changed {
		current.failure = errors.Join(current.failure, batch.Execute())
	}
}

func (current *previewer) clear(previews map[string]*edit_preview) {
	if len(previews) == 0 {
		return
	}
	batch := current.client.NewBatch()
	for id := range previews {
		delete(current.previews, id)
		batch.ExecLua(`require("the-agent.follow").clear(...)`, nil, id)
	}
	current.failure = errors.Join(current.failure, batch.Execute())
}

func (current *previewer) finish() error {
	close(current.stop)
	<-current.done
	current.mutex.Lock()
	defer current.mutex.Unlock()
	current.clear(current.previews)
	return current.failure
}
