if vim.g.loaded_the_agent then
  return
end
vim.g.loaded_the_agent = true

vim.api.nvim_create_user_command("TA", function(args)
  require("the-agent").open(args.args)
end, { nargs = 1, desc = "the-agent: create or open a session" })

-- Thinking folds closed whenever a session (or subagent) file is shown.
vim.api.nvim_create_autocmd("BufWinEnter", {
  group = vim.api.nvim_create_augroup("the-agent", { clear = true }),
  pattern = "*/.the-agent/sessions/*session.md",
  callback = function(args)
    require("the-agent").fold_thinking(args.buf)
  end,
  desc = "the-agent: fold thinking blocks",
})

vim.api.nvim_create_user_command("TASend", function()
  require("the-agent").send()
end, { nargs = 0, desc = "the-agent: send the current session" })

vim.api.nvim_create_user_command("TAAbort", function()
  require("the-agent").abort()
end, { nargs = 0, desc = "the-agent: abort the current session's turn" })
