# Dogfooding implementation

Starting point: c3933b1. README.md must remain unchanged. Implement the six
requested areas, with separate atomic commits for completed tasks/subtasks.
All lifecycle rehearsals use disposable homes; this work does not install into
the maintainer's actual shell, choose a license, publish, or download models.

## Requirements and verification

- [ ] 1. Install/upgrade/disable/remove wizard and CLI: one managed shell block,
  ZDOTDIR support, backups, idempotence, symlink preservation, safe undo,
  retained preferences by default and explicit purge. Verify interrupted work,
  changed files, repeat operations and actual interactive-shell activation.
- [ ] 2. Doctor: meaningful pass/warning/failure checks and exit status, exact
  causes/actions, shell activation/order/keymaps/async mode, component versions
  and stale sockets; conservative repair with backups and undo.
- [ ] 3. Typed config show/validate/set/reset/repair: effective/default/source
  information, path/line/key diagnostics, preserve valid settings/comments,
  validate before atomic writes, handle malformed config without blanket reset.
- [ ] 4. Wizard-exposed customization: providers/budgets, message length,
  capitalization/scopes, candidate count, bindings, and learning. Preferences
  influence generation while grounding remains mandatory.
- [ ] 5. Global/repository enable-disable and temporary snooze, immediate cached
  lookup enforcement, local explicit feedback independent of inferred learning.
- [ ] 6. Lifecycle tests: interruption, repeat setup, symlinked dotfiles,
  broken config, old running daemon, rollback with subsequent user edits,
  uninstall and end-to-end installation-to-removal rehearsal.

## Approach

Build shared atomic-edit/journal and schema primitives before wiring the wizard,
repair and undo commands. Keep writes explicit and noninteractive execution
deterministic. Test both normal and failure paths, then run the complete existing
Go/Zsh/Neovim/install/race/fuzz/static checks. Confirm README and a clean tree
before declaring completion.

## Progress

- Current tree clean. No applicable AGENTS.md files found.
- Reviewed official Zsh startup-file/option documentation and Go/TOML APIs.
