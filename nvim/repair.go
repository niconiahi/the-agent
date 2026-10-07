package nvim

import (
	neovim "github.com/neovim/go-client/nvim"
)

// repair_buffer removes from buffer the orphaned tool blocks that the request
// left out: sent is the session as it was sent, repaired the same session
// after session.Repair. It runs once the turn has ended and the buffer is
// unlocked, so the repair is the last change of the turn: a single u brings
// back what it removed, whatever the streaming before it did. The removed
// lines all lie above the last user turn, so the turn streamed below it
// doesn't move them. The buffer is saved.
func repair_buffer(client *neovim.Nvim, buffer neovim.Buffer, sent string, repaired string) error {
	if sent == repaired {
		return nil
	}
	return client.ExecLua(`
		local buffer, before, after = ...
		local diff = (vim.text and vim.text.diff) or vim.diff
		local lines = vim.split(after, "\n", { plain = true })
		local hunks = diff(before, after, { result_type = "indices" })
		-- Bottom up, so earlier hunks keep their line numbers.
		for index = #hunks, 1, -1 do
			local old_start, old_count, new_start, new_count = unpack(hunks[index])
			local start = old_count == 0 and old_start or old_start - 1
			vim.api.nvim_buf_set_lines(buffer, start, start + old_count, true,
				vim.list_slice(lines, new_start, new_start + new_count - 1))
		end
		vim.api.nvim_buf_call(buffer, function() vim.cmd("silent write") end)
	`, nil, int(buffer), sent, repaired)
}
