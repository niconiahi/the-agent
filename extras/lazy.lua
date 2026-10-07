-- lazy.nvim spec for the-agent. Copy it to
-- ~/.config/nvim/lua/plugins/the-agent.lua (needs Go on PATH to build).
--
-- The keys live in lazy.nvim, so pressing one also loads the plugin and
-- which-key shows them under LazyVim's <leader>a ("ai") group. Override or
-- disable them the lazy.nvim way, e.g. { "<leader>as", false }.
return {
  "niconiahi/the-agent",
  main = "the-agent",
  build = "go build -o bin/the-agent ./cmd/agent",
  cmd = { "TA", "TASend", "TAAbort" },
  keys = {
    { "<leader>aa", "<Plug>(TA)", remap = true, desc = "Session (the-agent)" },
    { "<leader>as", "<Plug>(TASend)", remap = true, desc = "Send session (the-agent)" },
    { "<leader>ax", "<Plug>(TAAbort)", remap = true, desc = "Abort turn (the-agent)" },
  },
  opts = {
    -- lazy.nvim owns the keys above; don't map the plugin's defaults too.
    keys = false,
    -- ceiling = 200000,
    -- name = function(input) return os.date("%Y-%m-%d") .. "-" .. input end,
  },
}
