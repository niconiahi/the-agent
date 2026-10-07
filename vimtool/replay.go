package vimtool

import (
	"context"

	neovim "github.com/neovim/go-client/nvim"

	"github.com/niconiahi/the-agent/tool"
)

func Replay(client *neovim.Nvim) tool.Replay {
	return tool.Replay{
		Write: func(invocation_context context.Context, path string, content string) error {
			_, error := change_buffer(client, invocation_context, 0, "write", path, content)
			return error
		},
		Delete: func(invocation_context context.Context, path string) error {
			agent := SessionDirectory(invocation_context)
			_, error := leased_change(client, agent, path, "delete", path, agent, sidecar_timestamp(invocation_context))
			return error
		},
	}
}
