package vimtool

import (
	"context"
	"errors"
	"time"

	neovim "github.com/neovim/go-client/nvim"

	"github.com/niconiahi/the-agent/session"
	"github.com/niconiahi/the-agent/tool"
)

type session_key struct{}

type turn struct {
	directory string
	now       func() time.Time
}

func WithSession(invocation_context context.Context, directory string, now func() time.Time) context.Context {
	return context.WithValue(invocation_context, session_key{}, turn{directory: directory, now: now})
}

func session_directory(invocation_context context.Context) string {
	current, _ := invocation_context.Value(session_key{}).(turn)
	return current.directory
}

func stamp(invocation_context context.Context) string {
	current, _ := invocation_context.Value(session_key{}).(turn)
	now := time.Now
	if current.now != nil {
		now = current.now
	}
	return now().UTC().Format(session.TIMESTAMP_FORMAT)
}

func Tools(client *neovim.Nvim) []tool.Tool {
	return []tool.Tool{Read(client), Edit(client), Write(client), Filter(client)}
}

type region struct {
	Buffer int `msgpack:"buffer"`
	Mark   int `msgpack:"mark"`
}

type outcome struct {
	Content string `msgpack:"content"`
	Error   string `msgpack:"error"`
	Region  region `msgpack:"region"`
}

func call(client *neovim.Nvim, function string, arguments ...any) (outcome, error) {
	var result outcome
	code := `local name = ...; return require("the-agent.buffer")[name](select(2, ...))`
	if error := client.ExecLua(code, &result, append([]any{function}, arguments...)...); error != nil {
		return outcome{}, error
	}
	if result.Error != "" {
		return outcome{}, errors.New(result.Error)
	}
	return result, nil
}

func change(client *neovim.Nvim, invocation_context context.Context, function string, arguments ...any) error {
	changed, error := call(client, function, append(arguments, session_directory(invocation_context), stamp(invocation_context))...)
	if error != nil {
		return error
	}
	_, error = call(client, "release", changed.Region)
	return error
}
