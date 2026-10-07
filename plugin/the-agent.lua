if vim.g.loaded_the_agent then
  return
end
vim.g.loaded_the_agent = true

vim.api.nvim_create_user_command("TA", function(args)
  require("the-agent").open(args.args)
end, { nargs = "?", desc = "the-agent: create or open a session" })

vim.api.nvim_create_user_command("TASend", function()
  require("the-agent").send()
end, { nargs = 0, desc = "the-agent: send the current session" })

vim.api.nvim_create_user_command("TAAbort", function()
  require("the-agent").abort()
end, { nargs = 0, desc = "the-agent: abort the current session's turn" })

-- <Plug> mappings, the stable targets for your own keys. setup() maps the
-- default keys (config.keys) onto them.
vim.keymap.set("n", "<Plug>(TA)", function()
  require("the-agent").prompt()
end, { desc = "the-agent: create or open a session" })
vim.keymap.set("n", "<Plug>(TASend)", function()
  require("the-agent").send()
end, { desc = "the-agent: send the current session" })
vim.keymap.set("n", "<Plug>(TAAbort)", function()
  require("the-agent").abort()
end, { desc = "the-agent: abort the current session's turn" })

local group = vim.api.nvim_create_augroup("the-agent", { clear = true })
-- `*` matches `/` in autocmd patterns, so subagent sessions match too.
local SESSION_PATTERN = "*/.the-agent/sessions/*session.md"

-- Thinking folds closed whenever a session (or subagent) file is shown.
vim.api.nvim_create_autocmd("BufWinEnter", {
  group = group,
  pattern = SESSION_PATTERN,
  callback = function(args)
    require("the-agent").fold_thinking(args.buf)
  end,
  desc = "the-agent: fold thinking blocks",
})

-- The follow window follows the session I was last in.
vim.api.nvim_create_autocmd("BufEnter", {
  group = group,
  pattern = SESSION_PATTERN,
  callback = function(args)
    require("the-agent.follow").enter(args.buf)
  end,
  desc = "the-agent: follow this session's edits",
})

local function highlights()
  vim.api.nvim_set_hl(0, "TheAgentTokensNear", { link = "DiagnosticError", default = true })
end
highlights()
vim.api.nvim_create_autocmd("ColorScheme", { group = group, callback = highlights })

vim.api.nvim_create_autocmd({ "BufEnter", "TextChanged", "InsertLeave", "BufWritePost" }, {
  group = group,
  pattern = SESSION_PATTERN,
  callback = function(args)
    require("the-agent").refresh(args.buf)
  end,
  desc = "the-agent: recount the session's tokens",
})

vim.api.nvim_create_autocmd("BufWinEnter", {
  group = group,
  pattern = SESSION_PATTERN,
  callback = function()
    local agent = require("the-agent")
    local default = vim.api.nvim_get_option_info2("statusline", {}).default
    local window = vim.api.nvim_get_option_value("statusline", { scope = "local" })
    local untouched = vim.go.statusline == default and (window == "" or window == agent.STATUSLINE)
    if agent.config.statusline and untouched then
      vim.opt_local.statusline = agent.STATUSLINE
    end
  end,
  desc = "the-agent: show the token count in the statusline",
})
