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

local function missing(path)
  return { error = "failed to read file: " .. path .. ": no such file" }
end

-- Writes the user's unsaved version of buffer to
-- <session>/unsaved/<path relative to the cwd>.<stamp> and returns that path.
local function set_aside(buffer, path, agent, stamp)
  local relative = vim.fn.fnamemodify(path, ":."):gsub("^/+", "")
  local sidecar = agent .. "/unsaved/" .. relative .. "." .. stamp
  vim.fn.mkdir(vim.fn.fnamemodify(sidecar, ":h"), "p")
  local file, message = io.open(sidecar, "wb")
  if not file then
    return nil, "failed to save your unsaved changes: " .. message
  end
  file:write(content(buffer))
  file:close()
  return sidecar
end

-- Gets the buffer for path ready for an agent change: the loaded buffer or
-- a newly loaded one, holding what is on disk. Unsaved changes of the user
-- go to a sidecar under the agent's session first, and the user is told
-- where; the buffer is then reloaded so the change never mixes with them.
local function prepare(path, agent, stamp)
  local buffer = find(path) or load(path)
  if not vim.bo[buffer].modified then
    refresh(buffer)
    return buffer
  end
  if agent == nil or agent == "" then
    return nil, "the file has unsaved changes in the editor"
  end
  local sidecar, failure = set_aside(buffer, path, agent, stamp)
  if not sidecar then
    return nil, failure
  end
  vim.api.nvim_buf_call(buffer, function()
    vim.cmd("silent edit!")
  end)
  require("the-agent").notify(
    "your unsaved changes to " .. vim.fn.fnamemodify(path, ":.") .. " were saved to " .. sidecar,
    vim.log.levels.WARN
  )
  return buffer
end

-- Marks the changed text with an extmark, saves and records the agent's
-- tick. The region lets the caller inspect the change (e.g. for
-- diagnostics) before releasing it.
local function finish(buffer, agent, start_row, start_col, end_row, end_col)
  local mark = vim.api.nvim_buf_set_extmark(buffer, NAMESPACE, start_row, start_col, {
    end_row = end_row,
    end_col = end_col,
    right_gravity = false,
    end_right_gravity = true,
  })
  save(buffer)
  record(buffer, agent)
  return { region = { buffer = buffer, mark = mark } }
end

-- Replaces the one occurrence of old_text, marks the new text with an
-- extmark and saves.
function M.edit(path, old_text, new_text, agent, stamp)
  if old_text == "" then
    return { error = "old_text must not be empty" }
  end
  path = resolve(path)
  if not find(path) and vim.fn.filereadable(path) == 0 then
    return missing(path)
  end
  local buffer, failure = prepare(path, agent, stamp)
  if not buffer then
    return { error = failure }
  end

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
  return finish(buffer, agent, start_row, start_col, start_row + #lines - 1, (#lines == 1 and start_col or 0) + #lines[#lines])
end

-- Sets the buffer's whole content, creating the file (and its directory)
-- when it does not exist yet.
function M.write(path, text, agent, stamp)
  path = resolve(path)
  vim.fn.mkdir(vim.fn.fnamemodify(path, ":h"), "p")
  local buffer, failure = prepare(path, agent, stamp)
  if not buffer then
    return { error = failure }
  end
  local ends = text:sub(-1) == "\n"
  local lines = vim.split(ends and text:sub(1, -2) or text, "\n", { plain = true })
  vim.api.nvim_buf_set_lines(buffer, 0, -1, true, lines)
  if not ends and text ~= "" then
    vim.bo[buffer].fixeol = false
  end
  vim.bo[buffer].eol = ends
  return finish(buffer, agent, 0, 0, #lines - 1, #lines[#lines])
end

-- Runs command over the whole buffer like :%!command. A command that fails
-- is undone at once, so it leaves neither text nor an undo step behind.
function M.filter(path, command, agent, stamp)
  path = resolve(path)
  if not find(path) and vim.fn.filereadable(path) == 0 then
    return missing(path)
  end
  local buffer, failure = prepare(path, agent, stamp)
  if not buffer then
    return { error = failure }
  end
  vim.api.nvim_buf_call(buffer, function()
    vim.cmd("silent %!" .. vim.fn.escape(command, "%#!"))
  end)
  if vim.v.shell_error ~= 0 then
    local output = content(buffer)
    local code = vim.v.shell_error
    vim.api.nvim_buf_call(buffer, function()
      vim.cmd("silent undo")
    end)
    return { error = string.format("%s exited with %d: %s", command, code, output) }
  end
  local last_row = vim.api.nvim_buf_line_count(buffer) - 1
  local last_line = vim.api.nvim_buf_get_lines(buffer, last_row, last_row + 1, true)[1]
  return finish(buffer, agent, 0, 0, last_row, #last_line)
end

function M.release(region)
  pcall(vim.api.nvim_buf_del_extmark, region.buffer, NAMESPACE, region.mark)
  return vim.empty_dict()
end

return M
