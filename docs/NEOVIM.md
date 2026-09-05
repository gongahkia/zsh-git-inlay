# Neovim event adapter

The Neovim adapter is an optional activity producer, not an editor frontend. It
does not generate or render commit messages, call a provider, scan buffers, or
maintain its own context store. The existing Zsh/autosuggestions flow remains
the only suggestion frontend.

It requires Neovim 0.10 or later and the `zsh-git-inlay` executable on `PATH`.
Activity must be granted first; without it, the local emitter exits before
reading Git state and the daemon retains no event.

```zsh
zsh-git-inlay permissions enable activity
```

Manual setup, after adding this repository to Neovim's runtime path:

```lua
vim.opt.rtp:append("/absolute/path/to/zsh-git-inlay")

require("zsh-git-inlay").setup({
  file_events = true,
  diagnostics = true,
})
```

For `lazy.nvim`:

```lua
{
  dir = "/absolute/path/to/zsh-git-inlay",
  config = function()
    require("zsh-git-inlay").setup()
  end,
}
```

For `packer.nvim`:

```lua
use({
  "/absolute/path/to/zsh-git-inlay",
  config = function()
    require("zsh-git-inlay").setup()
  end,
})
```

Use an explicit executable path when necessary:

```lua
require("zsh-git-inlay").setup({
  executable = "/home/me/.local/bin/zsh-git-inlay",
  max_diagnostics = 1024,
})
```

The adapter emits only `editor.file_opened`, `editor.file_saved`, and
`lsp.diagnostics_changed`. File events carry the buffer path and a per-Neovim
session ID. Diagnostic transitions carry capped error/warning/info/hint counts,
not diagnostic prose, source, or buffer content. Each event is sent
asynchronously through the existing local activity emitter, which supplies and
verifies the repository/worktree identity. Event failure is ignored by Neovim.

Disable and remove all adapter autocmds without touching other configuration:

```lua
require("zsh-git-inlay").disable()
```

Run `:checkhealth zsh-git-inlay` to verify the executable and activity-grant
status. The checked-in headless test is run with `make test` on this host; it
uses Neovim 0.11.6 and proves the adapter emits bounded kinds/counts, omits
diagnostic text, contains emitter failures, and stops after `disable()`. On a
host without Neovim, the `test-nvim` target reports that it was skipped.
