if vim.g.loaded_the_agent then
  return
end
vim.g.loaded_the_agent = true

vim.api.nvim_create_user_command("TA", function(args)
  require("the-agent").open(args.args)
end, { nargs = 1, desc = "the-agent: create or open a session" })

vim.api.nvim_create_user_command("TASend", function()
  require("the-agent").send()
end, { nargs = 0, desc = "the-agent: send the current session" })

local function highlights()
  vim.api.nvim_set_hl(0, "TheAgentTokensNear", { link = "DiagnosticError", default = true })
end
highlights()

local group = vim.api.nvim_create_augroup("the-agent", { clear = true })
-- `*` matches `/` in autocmd patterns, so subagent sessions match too.
local SESSION_PATTERN = "*/.the-agent/sessions/*/session.md"

vim.api.nvim_create_autocmd("ColorScheme", { group = group, callback = highlights })

vim.api.nvim_create_autocmd({ "BufEnter", "TextChanged", "InsertLeave", "BufWritePost" }, {
  group = group,
  pattern = SESSION_PATTERN,
  callback = function(args)
    require("the-agent").refresh(args.buf)
  end,
})

vim.api.nvim_create_autocmd("BufWinEnter", {
  group = group,
  pattern = SESSION_PATTERN,
  callback = function()
    local agent = require("the-agent")
    if agent.config.statusline then
      vim.opt_local.statusline = agent.STATUSLINE
    end
  end,
})
