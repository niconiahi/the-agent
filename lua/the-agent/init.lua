-- the-agent: thin Neovim side. Commands forward to the Go binary over
-- msgpack-RPC; Go drives the buffers.
local M = {}

local plugin_root = vim.fn.fnamemodify(debug.getinfo(1, "S").source:sub(2), ":h:h:h")

M.config = {
  -- Path to the binary. Defaults to bin/the-agent inside the plugin, which is
  -- what `go build -o bin/the-agent ./cmd/agent` produces.
  bin = plugin_root .. "/bin/the-agent",
  -- An already open RPC channel to the Go side (used by tests). When nil the
  -- binary is started on first use.
  chan = nil,
}

function M.setup(opts)
  M.config = vim.tbl_extend("force", M.config, opts or {})
end

function M.notify(msg, level)
  vim.notify(msg, level or vim.log.levels.INFO, { title = "the-agent" })
end

-- Line ranges ({ first, last }, 1-based) of the ```thinking blocks in lines,
-- skipping fences nested in other code blocks.
local function thinking_ranges(lines)
  local ranges, open = {}, nil
  for index, line in ipairs(lines) do
    if open then
      local ticks = line:match("^%s*(`+)%s*$")
      if ticks and #ticks >= open.run then
        if open.thinking then
          table.insert(ranges, { open.start, index })
        end
        open = nil
      end
    else
      local ticks, info = line:match("^ *(```+)(.*)$")
      if ticks then
        open = { run = #ticks, start = index, thinking = vim.split(vim.trim(info), "%s+")[1] == "thinking" }
      end
    end
  end
  return ranges
end

-- Folds the thinking blocks of buf closed in every window showing it. With
-- from (a 1-based line), only blocks starting at or after it are folded and
-- existing folds are kept; without it, the window's folds are rebuilt.
function M.fold_thinking(buf, from)
  buf = (buf == nil or buf == 0) and vim.api.nvim_get_current_buf() or buf
  local ranges = thinking_ranges(vim.api.nvim_buf_get_lines(buf, 0, -1, false))
  for _, win in ipairs(vim.fn.win_findbuf(buf)) do
    vim.api.nvim_win_call(win, function()
      if vim.wo.foldmethod ~= "manual" then
        vim.wo.foldmethod = "manual"
      end
      if not from then
        vim.cmd("normal! zE")
      end
      for _, range in ipairs(ranges) do
        if range[1] >= (from or 1) then
          vim.cmd(string.format("%d,%dfold", range[1], range[2]))
          vim.cmd(string.format("%dfoldclose", range[1]))
        end
      end
    end)
  end
end

local function channel()
  if M.config.chan then
    return M.config.chan
  end
  local chan = vim.fn.jobstart({ M.config.bin, "--nvim" }, {
    rpc = true,
    cwd = vim.fn.getcwd(),
    on_stderr = function(_, data)
      local text = table.concat(data or {}, "\n")
      if text:match("%S") then
        M.notify(text, vim.log.levels.WARN)
      end
    end,
  })
  if chan <= 0 then
    error("the-agent: could not start " .. M.config.bin)
  end
  M.config.chan = chan
  return chan
end

function M.open(name)
  vim.rpcrequest(channel(), "the_agent_open", name)
end

function M.send()
  vim.rpcrequest(channel(), "the_agent_send", vim.api.nvim_get_current_buf())
end

function M.abort()
  vim.rpcrequest(channel(), "the_agent_abort", vim.api.nvim_get_current_buf())
end

return M
