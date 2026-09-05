local root = assert(arg[1], "repository root is required")
package.path = root .. "/lua/?.lua;" .. root .. "/lua/?/init.lua;" .. package.path

local calls = {}
local original_system = vim.system
vim.system = function(command, _)
  table.insert(calls, command)
  return { wait = function() return { code = 0, stdout = '{"activity":true}' } end }
end

local adapter = require("zsh-git-inlay")
assert(require("zsh-git-inlay.health"), "health module did not load")
adapter.setup({ executable = "zsh-git-inlay-test", file_events = true, diagnostics = true })
local buffer = vim.api.nvim_create_buf(true, false)
vim.api.nvim_buf_set_name(buffer, root .. "/tests/neovim-fixture.lua")
vim.api.nvim_exec_autocmds("BufReadPost", { buffer = buffer })
vim.api.nvim_exec_autocmds("BufWritePost", { buffer = buffer })
local namespace = vim.api.nvim_create_namespace("zsh-git-inlay-test")
vim.diagnostic.set(namespace, buffer, {
  { lnum = 0, col = 0, severity = vim.diagnostic.severity.ERROR, message = "DO-NOT-RETAIN" },
})

vim.wait(50, function() return #calls >= 3 end)
local kinds = {}
local diagnostic_counts = false
for _, command in ipairs(calls) do
  local joined = table.concat(command, "\0")
  assert(not joined:find("DO-NOT-RETAIN", 1, true), "diagnostic text reached the emitter")
  for index, value in ipairs(command) do
    if value == "--kind" then
      kinds[command[index + 1]] = true
      if command[index + 1] == "lsp.diagnostics_changed" then
        diagnostic_counts = joined:find("error_count=1", 1, true) ~= nil
      end
    end
  end
end
assert(kinds["editor.file_opened"] and kinds["editor.file_saved"] and kinds["lsp.diagnostics_changed"], "expected bounded editor events")
assert(diagnostic_counts, "expected diagnostic counts instead of prose")
local before = #calls
adapter.disable()
vim.api.nvim_exec_autocmds("BufWritePost", { buffer = buffer })
vim.wait(20)
assert(#calls == before, "disabled adapter emitted an event")

adapter.setup({ executable = "zsh-git-inlay-test" })
vim.system = function()
  error("simulated emitter failure")
end
assert(pcall(vim.api.nvim_exec_autocmds, "BufWritePost", { buffer = buffer }), "emitter failure broke Neovim")
adapter.disable()
vim.system = original_system
