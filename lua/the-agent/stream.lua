-- Buffer edits Go makes while a turn streams into a session.md. Each call is
-- one RPC, so an edit is atomic from the user's point of view.
local M = {}

-- Replaces lines [first, last) of buf with lines.
function M.set_lines(buf, first, last, lines)
  vim.api.nvim_buf_set_lines(buf, first, last, true, lines)
end

-- Saves buf to disk.
function M.write(buf)
  vim.api.nvim_buf_call(buf, function()
    vim.cmd("silent write")
  end)
end

return M
