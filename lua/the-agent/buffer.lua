-- Buffer-backed file operations for the agent's tools (vimtool). Each public
-- function runs inside one RPC request, so everything it changes is one undo
-- block. Failures come back as { error = "..." } for the tool result.
local M = {}

local NAMESPACE = vim.api.nvim_create_namespace("the-agent-edit")

local function resolve(path)
  return vim.fn.fnamemodify(path, ":p")
end

local function find(path)
  for _, buffer in ipairs(vim.api.nvim_list_bufs()) do
    if vim.api.nvim_buf_get_name(buffer) == path and vim.api.nvim_buf_is_loaded(buffer) then
      return buffer
    end
  end
end

-- Loads a file into a listed buffer without showing it in a window. An
-- existing swapfile must not stop the load with the ATTENTION prompt.
local function load(path)
  local buffer = vim.fn.bufadd(path)
  vim.bo[buffer].buflisted = true
  local shortmess = vim.o.shortmess
  vim.opt.shortmess:append("A")
  vim.fn.bufload(buffer)
  vim.o.shortmess = shortmess
  return buffer
end

local function read_disk(path)
  local file, message = io.open(path, "rb")
  if not file then
    return nil, message
  end
  local content = file:read("a")
  file:close()
  return content
end

-- The buffer as file contents: lines joined by newlines, plus the final
-- newline when the file has one.
local function content(buffer)
  local lines = vim.api.nvim_buf_get_lines(buffer, 0, -1, true)
  local text = table.concat(lines, "\n")
  if (#lines > 1 or text ~= "") and vim.bo[buffer].eol then
    text = text .. "\n"
  end
  return text
end

-- Picks up changes made on disk while the buffer was clean, so neither
-- reads nor writes act on stale text (a stale :write blocks on a prompt).
local function refresh(buffer)
  if not vim.bo[buffer].modified then
    vim.cmd("silent! checktime " .. buffer)
  end
end

-- Remembers the changedtick each agent last saw, keyed by its session
-- directory, for the cross-agent staleness check.
local function record(buffer, agent)
  if agent == nil or agent == "" then
    return
  end
  local ticks = vim.b[buffer].the_agent_ticks or {}
  ticks[agent] = vim.api.nvim_buf_get_changedtick(buffer)
  vim.b[buffer].the_agent_ticks = ticks
end

function M.read(path, agent)
  path = resolve(path)
  local buffer = find(path)
  if buffer then
    refresh(buffer)
    record(buffer, agent)
    if not vim.bo[buffer].modified then
      return { content = content(buffer) }
    end
  end
  local text, message = read_disk(path)
  if not text then
    return { error = "failed to read file: " .. message }
  end
  return { content = text }
end

local function occurrences(text, wanted)
  local first, count, from = nil, 0, 1
  while true do
    local start = text:find(wanted, from, true)
    if not start then
      return first, count
    end
    first = first or start
    count = count + 1
    from = start + #wanted
  end
end

-- 0-based row and byte column of the position after `offset` bytes.
local function position(text, offset)
  local before = text:sub(1, offset)
  local _, row = before:gsub("\n", "")
  local line_start = before:match(".*\n()") or 1
  return row, offset - line_start + 1
end

local function save(buffer)
  vim.api.nvim_buf_call(buffer, function()
    vim.cmd("silent write!")
  end)
end

-- Replaces the one occurrence of old_text, marks the new text with an
-- extmark and saves. Returns the region so the caller can inspect it (e.g.
-- for diagnostics) before releasing it.
function M.edit(path, old_text, new_text, agent)
  if old_text == "" then
    return { error = "old_text must not be empty" }
  end
  path = resolve(path)
  local buffer = find(path)
  if not buffer then
    if vim.fn.filereadable(path) == 0 then
      return { error = "failed to read file: " .. path .. ": no such file" }
    end
    buffer = load(path)
  end
  if vim.bo[buffer].modified then
    return { error = "the file has unsaved changes in the editor" }
  end
  refresh(buffer)

  local text = content(buffer)
  local first, count = occurrences(text, old_text)
  if count == 0 then
    return { error = "old_text not found in file" }
  end
  if count > 1 then
    return { error = string.format("old_text found %d times, must be unique", count) }
  end

  local start_row, start_col = position(text, first - 1)
  local end_row, end_col = position(text, first - 1 + #old_text)
  local last_row = vim.api.nvim_buf_line_count(buffer) - 1
  if end_row > last_row then
    -- old_text runs through the file's final newline, which is no buffer line.
    end_row = last_row
    end_col = #vim.api.nvim_buf_get_lines(buffer, last_row, last_row + 1, true)[1]
    new_text = (new_text:gsub("\n$", ""))
  end

  local lines = vim.split(new_text, "\n", { plain = true })
  vim.api.nvim_buf_set_text(buffer, start_row, start_col, end_row, end_col, lines)
  local mark = vim.api.nvim_buf_set_extmark(buffer, NAMESPACE, start_row, start_col, {
    end_row = start_row + #lines - 1,
    end_col = (#lines == 1 and start_col or 0) + #lines[#lines],
    right_gravity = false,
    end_right_gravity = true,
  })
  save(buffer)
  record(buffer, agent)
  return { region = { buffer = buffer, mark = mark } }
end

function M.release(region)
  pcall(vim.api.nvim_buf_del_extmark, region.buffer, NAMESPACE, region.mark)
  return vim.empty_dict()
end

return M
