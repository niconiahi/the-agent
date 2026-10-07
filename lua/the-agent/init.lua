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
  -- Token ceiling of a session. :TASend refuses above it. nil means 200k;
  -- it is always clamped to the model's context window.
  ceiling = nil,
  -- Set a statusline showing the token count on session windows, unless you
  -- already set one (a statusline plugin, or :set statusline). Either way
  -- require("the-agent").statusline() is the component to add to your own.
  statusline = true,
  -- Session naming: a function from what you typed after :TA (or into the
  -- <Plug>(TA) prompt, possibly "") to the session's name, e.g.
  --   name = function(input) return os.date("%Y-%m-%d") .. "-" .. input end
  -- nil uses the input as is. Either way "/" becomes "-".
  name = nil,
  -- Default normal-mode keys, each mapped to its <Plug>(TA…) mapping. Set one
  -- to false to leave it unmapped, or keys = false to map none. Under
  -- LazyVim they land in which-key's <leader>a ("ai") group.
  keys = {
    open = "<leader>aa", -- <Plug>(TA)
    send = "<leader>as", -- <Plug>(TASend)
    abort = "<leader>ax", -- <Plug>(TAAbort)
  },
}

local PLUGS = {
  open = { plug = "<Plug>(TA)", desc = "Session (the-agent)" },
  send = { plug = "<Plug>(TASend)", desc = "Send session (the-agent)" },
  abort = { plug = "<Plug>(TAAbort)", desc = "Abort turn (the-agent)" },
}

-- Keys mapped by the previous setup(), removed when setup() runs again.
local mapped = {}

local function map_keys()
  for _, lhs in ipairs(mapped) do
    pcall(vim.keymap.del, "n", lhs)
  end
  mapped = {}
  local keys = M.config.keys
  if not keys then
    return
  end
  for action, target in pairs(PLUGS) do
    local lhs = keys[action]
    if lhs then
      vim.keymap.set("n", lhs, target.plug, { remap = true, desc = target.desc })
      table.insert(mapped, lhs)
    end
  end
  local ok, which_key = pcall(require, "which-key")
  if ok and type(which_key.add) == "function" and #mapped > 0 then
    which_key.add({ { "<leader>a", group = "ai" } })
  end
end

function M.setup(opts)
  opts = opts or {}
  local keys = opts.keys
  opts.keys = nil
  M.config = vim.tbl_extend("force", M.config, opts)
  if keys ~= nil then
    -- Merge per key so overriding one keeps the other defaults.
    M.config.keys = keys and vim.tbl_extend("force", M.config.keys or {}, keys) or false
  end
  map_keys()
end

-- Notifies through snacks.nvim when it is installed, vim.notify otherwise.
function M.notify(msg, level)
  level = level or vim.log.levels.INFO
  local ok, snacks = pcall(require, "snacks")
  if ok and type(snacks) == "table" and type(snacks.notify) == "function" then
    snacks.notify(msg, { level = level, title = "the-agent" })
    return
  end
  vim.notify(msg, level, { title = "the-agent" })
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

-- Folds the thinking blocks of buf closed in every window showing it. Go
-- finds them. With from (a 1-based line), only blocks starting at or after it
-- are folded and existing folds are kept; without it, the window's folds are
-- rebuilt.
function M.fold_thinking(buf, from)
  buf = (buf == nil or buf == 0) and vim.api.nvim_get_current_buf() or buf
  local ranges = vim.rpcrequest(channel(), "the_agent_thinking", buf)
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

-- Opens (or creates) the session config.name makes of name.
function M.open(name)
  name = name or ""
  if M.config.name then
    name = M.config.name(name) or ""
  end
  vim.rpcrequest(channel(), "the_agent_open", name)
end

-- Asks for a session name, then opens it (what <Plug>(TA) does).
function M.prompt()
  vim.ui.input({ prompt = "the-agent session: " }, function(name)
    if name then
      M.open(name)
    end
  end)
end

function M.send()
  vim.rpcrequest(channel(), "the_agent_send", vim.api.nvim_get_current_buf())
end

function M.abort()
  vim.rpcrequest(channel(), "the_agent_abort", vim.api.nvim_get_current_buf())
end

-- Asks Go to recount a session buffer; the answer lands in b:the_agent_tokens.
function M.refresh(buf)
  vim.rpcnotify(channel(), "the_agent_count", buf or vim.api.nvim_get_current_buf())
end

-- Statusline component: the session's token count against its ceiling,
-- highlighted with TheAgentTokensNear (red) close to the ceiling. Empty for
-- buffers that aren't sessions. Usable from lualine and friends too.
function M.statusline()
  local win = vim.g.statusline_winid
  local buf = (win and vim.api.nvim_win_is_valid(win)) and vim.api.nvim_win_get_buf(win) or 0
  local tokens = vim.b[buf].the_agent_tokens
  if type(tokens) ~= "table" then
    return ""
  end
  if tokens.near then
    return "%#TheAgentTokensNear#" .. tokens.text .. "%*"
  end
  return tokens.text
end

-- The window-local statusline set on session buffers when config.statusline
-- is true: Neovim's default layout with the token count on the right.
M.STATUSLINE = "%<%f %h%m%r%=%{%v:lua.require'the-agent'.statusline()%}  %-14.(%l,%c%V%) %P"

return M
