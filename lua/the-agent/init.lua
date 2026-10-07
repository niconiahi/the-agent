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
