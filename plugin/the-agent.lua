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
