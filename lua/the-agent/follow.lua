-- The follow window and the streaming edit preview. Go calls preview() with
-- the fields of an edit call complete so far, at most once per flush, and
-- clear() when the call ends, fails or is aborted. The preview is only
-- extmarks: it never changes a buffer's text.
local M = {}

-- The follow window: the dedicated window the agent travels in. It is never
-- the window I am in.
local function follow_window()
  local current = vim.api.nvim_get_current_win()
  if vim.w[current].the_agent_follow then
    -- I moved into it, so it is mine now; the agent gets another one.
    vim.w[current].the_agent_follow = nil
  end
  for _, win in ipairs(vim.api.nvim_list_wins()) do
    if vim.w[win].the_agent_follow then
      return win
    end
  end
end

-- Shows buffer in the follow window, opening one beside everything else
-- when there is none, without entering it.
local function show(buffer)
  local win = follow_window()
  if not win then
    win = vim.api.nvim_open_win(buffer, false, { split = "right", win = -1 })
    vim.w[win].the_agent_follow = true
  elseif vim.api.nvim_win_get_buf(win) ~= buffer then
    vim.api.nvim_win_set_buf(win, buffer)
  end
  return win
end

local NAMESPACE = vim.api.nvim_create_namespace("the-agent-preview")

-- The text about to be replaced, and the text replacing it.
vim.api.nvim_set_hl(0, "TheAgentPreviewOld", { link = "DiffDelete", default = true })
vim.api.nvim_set_hl(0, "TheAgentPreviewNew", { link = "DiffAdd", default = true })

-- Edit previews by tool call id: { path, buffer, region, text }, where
-- region and text are extmark ids. Extmarks, not line numbers, so the
-- preview stays on the right lines if the file shifts under it.
local previews = {}

-- Finds old_text in the buffer, highlights it and scrolls the follow window
-- to it. An old_text that isn't there once is left for the edit to report.
local function highlight(state, old_text)
  local region = require("the-agent.buffer").locate(state.buffer, old_text)
  if not region then
    return
  end
  state.region = vim.api.nvim_buf_set_extmark(state.buffer, NAMESPACE, region.start_row, region.start_col, {
    end_row = region.end_row,
    end_col = region.end_col,
    hl_group = "TheAgentPreviewOld",
    right_gravity = false,
    end_right_gravity = true,
  })
  local win = show(state.buffer)
  vim.api.nvim_win_set_cursor(win, { region.start_row + 1, region.start_col })
  vim.api.nvim_win_call(win, function()
    vim.cmd("normal! zz")
  end)
end

-- Draws new_text as virtual lines under the highlighted region, replacing
-- what was drawn before.
local function draw(state, new_text)
  local region = vim.api.nvim_buf_get_extmark_by_id(state.buffer, NAMESPACE, state.region, { details = true })
  if #region == 0 then
    return
  end
  local lines = {}
  for _, line in ipairs(vim.split(new_text, "\n", { plain = true })) do
    table.insert(lines, { { line, "TheAgentPreviewNew" } })
  end
  state.text = vim.api.nvim_buf_set_extmark(state.buffer, NAMESPACE, region[3].end_row, 0, {
    id = state.text,
    virt_lines = lines,
  })
end

function M.preview(id, fields)
  local state = previews[id] or {}
  previews[id] = state
  if fields.path and fields.path ~= state.path then
    state.path = fields.path
    state.buffer = require("the-agent.buffer").open(fields.path)
    if state.buffer then
      show(state.buffer)
    end
  end
  if not state.buffer then
    return
  end
  if fields.old_text and fields.old_text ~= "" and not state.region then
    highlight(state, fields.old_text)
  end
  if state.region and fields.new_text then
    draw(state, fields.new_text)
  end
end

function M.clear(id)
  local state = previews[id]
  previews[id] = nil
  if not (state and state.buffer and vim.api.nvim_buf_is_valid(state.buffer)) then
    return
  end
  for _, mark in ipairs({ state.region, state.text }) do
    vim.api.nvim_buf_del_extmark(state.buffer, NAMESPACE, mark)
  end
end

return M
