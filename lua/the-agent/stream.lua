-- Buffer edits Go makes while a turn streams into a session.md. The buffer
-- is locked (modifiable=false) for the whole turn; each edit unlocks it,
-- edits and locks it again within one RPC call, so the user never gets a
-- moment to type into it.
local M = {}

function M.lock(buf)
  vim.bo[buf].modifiable = false
end

-- Replaces lines [first, last) of the locked buf with lines. Windows whose
-- cursor was on the last line follow it; the others don't move. fold_from,
-- when not 0, is the line where Go wrote a (whole) thinking block: fold it.
function M.set_lines(buf, first, last, lines, fold_from)
  local bottom = vim.api.nvim_buf_line_count(buf)
  local followers = {}
  for _, win in ipairs(vim.fn.win_findbuf(buf)) do
    if vim.api.nvim_win_get_cursor(win)[1] == bottom then
      table.insert(followers, win)
    end
  end

  vim.bo[buf].modifiable = true
  local ok, err = pcall(vim.api.nvim_buf_set_lines, buf, first, last, true, lines)
  vim.bo[buf].modifiable = false
  if not ok then
    error(err, 0)
  end

  if fold_from and fold_from > 0 then
    require("the-agent").fold_thinking(buf, fold_from)
  end

  bottom = vim.api.nvim_buf_line_count(buf)
  for _, win in ipairs(followers) do
    vim.api.nvim_win_set_cursor(win, { bottom, 0 })
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
