package vimtool

import (
	"context"
	"errors"

	neovim "github.com/neovim/go-client/nvim"

	"github.com/niconiahi/the-agent/tool"
)

type session_key struct{}

func WithSession(invocation_context context.Context, directory string) context.Context {
	return context.WithValue(invocation_context, session_key{}, directory)
}

func session_directory(invocation_context context.Context) string {
	directory, _ := invocation_context.Value(session_key{}).(string)
	return directory
}

func Tools(client *neovim.Nvim) []tool.Tool {
	return []tool.Tool{Read(client), Edit(client), Write(client)}
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
	changed, error := call(client, function, append(arguments, session_directory(invocation_context))...)
	if error != nil {
		return error
	}
	_, error = call(client, "release", changed.Region)
	return error
}
