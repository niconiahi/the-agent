-- Buffer edits Go makes while a turn streams into a session.md. The buffer
-- is locked (modifiable=false) for the whole turn; each edit unlocks it,
-- edits and locks it again within one RPC call, so the user never gets a
-- moment to type into it.
local M = {}

function M.lock(buf)
  vim.bo[buf].modifiable = false
end

-- Replaces lines [first, last) of the locked buf with lines.
function M.set_lines(buf, first, last, lines)
  vim.bo[buf].modifiable = true
  local ok, err = pcall(vim.api.nvim_buf_set_lines, buf, first, last, true, lines)
  vim.bo[buf].modifiable = false
  if not ok then
    error(err, 0)
  end
end

-- Unlocks buf and saves it to disk.
function M.finish(buf)
  vim.bo[buf].modifiable = true
  vim.api.nvim_buf_call(buf, function()
    vim.cmd("silent write")
  end)
end

return M
