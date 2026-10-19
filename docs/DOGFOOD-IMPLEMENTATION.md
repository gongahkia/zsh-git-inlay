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
- [x] 2. Doctor: meaningful pass/warning/failure checks and exit status, exact
  causes/actions, shell activation/order/keymaps/async mode, component versions
  and stale sockets; conservative repair with backups and undo.
- [x] 3. Typed config show/validate/set/reset/repair: effective/default/source
  information, path/line/key diagnostics, preserve valid settings/comments,
  validate before atomic writes, handle malformed config without blanket reset.
- [x] 4. Wizard-exposed customization: providers/budgets, message length,
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
- Doctor now fails with the exact cause when global configuration is invalid.
- Recoverable atomic edits retain private before/after backups, preserve symlink
  dotfiles, reject concurrent/later edits, roll back interrupted multi-file
  writes, and support idempotent undo. Race-tested these failure paths.
- Typed config commands use pinned go-toml v2.4.3 (MIT); release snapshots,
  installers, Homebrew preparation and the SBOM include its notice/dependency.
  Repair quarantines invalid settings as comments, keeps valid choices, and
  reports path/line/key; all writes validate first and print backup/undo paths.
- Customization engine now layers user message preferences beneath repository
  overrides, adapts sentence/lower capitalization, bounded path-to-scope rules,
  scope omission and short factual subjects before grounding; count applies
  after ranking. Global learning disable prevents collection and reuse. Relevant
  configuration/provider/daemon/grounding/Git/CLI packages passed race tests.
  Wizard exposure remains part of task 4.
- Controls and explicit feedback are implemented; lookup checks persisted state
  independently of notifications, pending generation is cancellable, snoozes
  expire, and explicit usefulness labels contain only scoped hashes. Feedback
  retention is bounded and clearing leaves no backup copy. Integration rehearsal
  and doctor/wizard exposure remain to be completed.
- Lifecycle engine manages manifest-recorded files and exactly one shell block,
  preserves symlinked dotfiles, migrates literal legacy source lines only when
  requested, verifies artifact hashes/targets, refuses artifact symlinks, and
  supports idempotent install/upgrade/uninstall. Marked-block undo preserves
  unrelated later edits; interrupted merged undo also recovers. Race-tested the
  engine; installation/removal wizard and interactive-shell doctor are next.
- Startup discovery honors ZDOTDIR after .zshenv, following the
  [documented Zsh startup order](https://zsh.sourceforge.io/Doc/Release/Files.html).
  Bounded normal/login interactive probes report actual loaded components,
  PATH, strategies, async mode and bindings. Process-group cancellation and
  [Go exec WaitDelay](https://pkg.go.dev/os/exec#Cmd) bound stalled startup and
  inherited output pipes. Isolated-home race tests cover strict broken-config
  defaults, startup noise, output caps and hung startup children.
- Socket repair now verifies type, owner, private parent, connection refusal and
  unchanged inode. Live sockets, regular files and transient dial failures are
  preserved. This also replaces the daemon's previous any-dial-error cleanup.
- `setup --wizard` and install/upgrade/customize/repair commands now connect to
  the engine. Repeated typed `--set` values are validated together; the guided
  flow reviews choices before writing. Removal retains preferences/backups and
  a private recovery helper; explicit purge validates dedicated product paths.
  Isolated CLI tests cover cancellation, reviewed customization, invalid
  provider batches, idempotent setup, removal, undo, and purge. Race checks pass.
  Full real-shell lifecycle rehearsal and enhanced doctor remain outstanding.
- Daemon status now captures version/commit/protocol/plugin contract and the
  executable digest once at startup, so an older process can be identified
  after an upgrade. Recorded repair restores manifest-matching bytes from
  journal backups and preserves unmarked shell text; it does not bless a
  corrupted installed plugin as its own repair source. Undo retains that
  original broken state for inspection or rollback.
- Doctor now emits pass/warning/failure checks, exact actions and a failure exit
  for unresolved required checks; --check verifies real normal/login shells,
  active keymaps/conflicts, async mode, component consistency and source paths.
  --fix recovers interrupted writes, selectively repairs user/repo preferences,
  restores verified owned artifacts/blocks, repairs corrupt controls to disabled
  and restarts an older daemon. Durable changes include backup/undo records;
  ambiguous/unowned changes remain explicit failures.
- Read-only doctor/history inspection creates no runtime directories or lock
  files. Named pipes cannot hang config/source inspection. Private socket mode
  is correct at bind; shutdown cancels/drains jobs and requests before returning.
  Unit environments isolate HOME/XDG, credentials and Git configuration. A real
  test process is killed after its first write and recovery restores the entire
  transaction. Relevant package race tests and repeated shutdown tests pass.
