package vimtool

import (
	"context"
	"errors"
	"fmt"
	"time"

	neovim "github.com/neovim/go-client/nvim"

	"github.com/niconiahi/the-agent/session"
)

type session_key struct{}

type agent_session struct {
	directory string
	now       func() time.Time
}

func WithSession(invocation_context context.Context, directory string, now func() time.Time) context.Context {
	return context.WithValue(invocation_context, session_key{}, agent_session{directory: directory, now: now})
}

func SessionDirectory(invocation_context context.Context) string {
	current, _ := invocation_context.Value(session_key{}).(agent_session)
	return current.directory
}

func sidecar_timestamp(invocation_context context.Context) string {
	current, _ := invocation_context.Value(session_key{}).(agent_session)
	now := time.Now
	if current.now != nil {
		now = current.now
	}
	return now().UTC().Format(session.TIMESTAMP_FORMAT)
}

func required_string(arguments map[string]any, name string) (string, error) {
	value, ok := arguments[name].(string)
	if !ok {
		return "", fmt.Errorf("%s is required", name)
	}
	return value, nil
}

type region struct {
	Buffer int `msgpack:"buffer"`
	Mark   int `msgpack:"mark"`
}

type diagnostic struct {
	Line     int    `msgpack:"line"`
	Column   int    `msgpack:"column"`
	Severity string `msgpack:"severity"`
	Message  string `msgpack:"message"`
	Source   string `msgpack:"source"`
}

type buffer_result struct {
	Content     string       `msgpack:"content"`
	Error       string       `msgpack:"error"`
	Region      region       `msgpack:"region"`
	Diagnostics []diagnostic `msgpack:"diagnostics"`
}

func call_buffer_function(client *neovim.Nvim, function string, arguments ...any) (buffer_result, error) {
	var result buffer_result
	code := `local name = ...; return require("the-agent.buffer")[name](select(2, ...))`
	if error := client.ExecLua(code, &result, append([]any{function}, arguments...)...); error != nil {
		return buffer_result{}, error
	}
	if result.Error != "" {
		return buffer_result{}, errors.New(result.Error)
	}
	return result, nil
}

// change_buffer takes the calling agent's lease on path, then calls the
// Lua function that changes the buffer, saves it and locks it under the
// lease in one request, and closes the changed region.
func change_buffer(client *neovim.Nvim, invocation_context context.Context, diagnostics_wait time.Duration, function string, path string, arguments ...any) ([]diagnostic, error) {
	agent := SessionDirectory(invocation_context)
	give_back, error := lease(client, agent, path)
	if error != nil {
		return nil, error
	}
	arguments = append(append([]any{path}, arguments...), agent, sidecar_timestamp(invocation_context), agent != "")
	changed, error := call_buffer_function(client, function, arguments...)
	if error != nil {
		give_back()
		return nil, error
	}
	closed, error := call_buffer_function(client, "close_region", changed.Region, diagnostics_wait.Milliseconds())
	if error != nil {
		return nil, error
	}
	return closed.Diagnostics, nil
}
