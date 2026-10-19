# Dogfooding implementation

Starting point: c3933b1. README.md must remain unchanged. Implement the six
requested areas, with separate atomic commits for completed tasks/subtasks.
All lifecycle rehearsals use disposable homes; this work does not install into
the maintainer's actual shell, choose a license, publish, or download models.

## Requirements and verification

- [x] 1. Install/upgrade/disable/remove wizard and CLI: one managed shell block,
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
- [x] 5. Global/repository enable-disable and temporary snooze, immediate cached
  lookup enforcement, local explicit feedback independent of inferred learning.
- [x] 6. Lifecycle tests: interruption, repeat setup, symlinked dotfiles,
  broken config, old running daemon, rollback with subsequent user edits,
  uninstall and end-to-end installation-to-removal rehearsal.

## Approach

Build shared atomic-edit/journal and schema primitives before wiring the wizard,
repair and undo commands. Keep writes explicit and noninteractive execution
deterministic. Test both normal and failure paths, then run the complete existing
Go/Zsh/Neovim/install/race/fuzz/static checks. Confirm README and a clean tree
before declaring completion.

## Progress

- Starting tree was clean. No applicable AGENTS.md files found.
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
  The wizard and validated batch CLI expose these settings.
- Controls and explicit feedback are implemented; lookup checks persisted state
  independently of notifications, pending generation is cancellable, snoozes
  expire, and explicit usefulness labels contain only scoped hashes. Feedback
  retention is bounded and clearing leaves no backup copy. CLI and real-shell
  lifecycle rehearsals cover these controls and labels.
- Lifecycle engine manages manifest-recorded files and exactly one shell block,
  preserves symlinked dotfiles, migrates literal legacy source lines only when
  requested, verifies artifact hashes/targets, refuses artifact symlinks, and
  supports idempotent install/upgrade/uninstall. Marked-block undo preserves
  unrelated later edits; interrupted merged undo also recovers. Race-tested the
  engine, with installed normal/login shell verification in the CLI rehearsals.
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
  Full real-shell lifecycle rehearsals and enhanced doctor checks are included.
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
  and stops an older daemon so the next prompt starts the current helper.
  Durable changes include backup/undo records;
  ambiguous/unowned changes remain explicit failures.
- Read-only doctor/history inspection creates no runtime directories or lock
  files. Named pipes cannot hang config/source inspection. Private socket mode
  is correct at bind; shutdown cancels/drains jobs and requests before returning.
  Unit environments isolate HOME/XDG, credentials and Git configuration. A real
  test process is killed after its first write and recovery restores the entire
  transaction. Relevant package race tests and repeated shutdown tests pass.
- Real-helper lifecycle rehearsals pass with zsh-autosuggestions v0.7.1: first
  actual shell suggestion, redirected ZDOTDIR and symlinked rc, tuning/scopes,
  explicit feedback, cached disable/snooze, broken config and artifact repair,
  an older running executable whose on-disk file has been replaced, safe refusal
  of duplicate blocks, removal, executable recovery/undo, and explicit purge.
  Sentinel files and staged Git changes survive throughout.
- The complete local `make test lint` suite passed, including Go, Zsh/PTY,
  Neovim, release reproducibility/installation, soak and lifecycle checks.
  A regression test also exposed ambient preferences affecting the explicit
  evaluation baseline; generation now uses its supplied settings beneath repo
  overrides. Relevant configuration/daemon/evaluation race checks pass.
- Test runners also remove inherited Git directory/index/object/config
  overrides before creating fixtures. A regression test checks isolation and
  restoration of the caller's environment; a private HOME alone is insufficient
  to prevent an inherited `GIT_INDEX_FILE` from redirecting fixture writes.

## Local validation

Validated on macOS/amd64 with Go 1.27.0, Zsh 5.9 and
zsh-autosuggestions v0.7.1:

- `make test lint` passed: Go tests, packaging, actual shell/PTY integration,
  crash recovery, evaluation, Neovim, reproducible four-target release snapshots,
  installation/removal, multi-shell soak and both lifecycle rehearsals.
- `go test -race ./...` passed, including the inherited-environment regression.
- `make fuzz FUZZ_TIME=1s` passed all seven bounded fuzz targets, including
  configuration parsing/repair after the literal-dotted-key regression fix.
- `actionlint` passed for the CI changes.
- A 10-sample `BenchmarkSuggestHelper` smoke run passed with both 10-record and
  10,000-record indexes. It ran under concurrent local test load; those timings
  are diagnostic samples rather than a latency guarantee.

Run the broad suites sequentially when reproducing these gates: overlapping
complete/race runs on this machine exceeded fixed Git snapshot deadlines.
Hosted CI execution and live-model suggestion usefulness remain unverified by
these local checks. The automated provider checks use local fixtures/mocks;
no model was downloaded and no cloud generation was requested.

## Try locally

Git, Zsh and zsh-autosuggestions are prerequisites. The wizard discovers common
dependency locations or accepts `--autosuggestions /absolute/path/to/source`.
For a missing dependency, follow its
[upstream installation guide](https://github.com/zsh-users/zsh-autosuggestions/blob/master/INSTALL.md).
The following commands intentionally install into your own shell; the automated
rehearsals above used disposable homes only.

```sh
make build
.build/zsh-git-inlay setup --wizard
```

Review the wizard's paths/preferences and apply. Open a new Zsh session, then
run these from a repository with staged changes:

```sh
zsh-git-inlay doctor --check
zsh-git-inlay observe --cwd .
zsh-git-inlay status --cwd .
```

Type `git commit -m ` and use your usual autosuggestion acceptance binding.
Default cycling is Ctrl-X, then g in emacs/vi insert mode. No commit is executed
by the suggestion provider. Deterministic generation is the default; choosing
Ollama exposes model/budget settings without downloading a model. Cloud use
still requires an explicit context grant and credential.

Use `setup --wizard` and choose customize for the guided preferences flow, or
make a validated batch directly:

```sh
zsh-git-inlay setup customize \
  --set commit.capitalization=sentence \
  --set commit.line_length=72 \
  --set suggestions.candidate_count=2
zsh-git-inlay config show
zsh-git-inlay feedback useful --candidate 1 --cwd .
zsh-git-inlay feedback too-generic --candidate 2 --cwd .
zsh-git-inlay feedback show --cwd .
zsh-git-inlay disable --repo --cwd .
zsh-git-inlay enable --repo --cwd .
zsh-git-inlay snooze 30m --repo --cwd .
zsh-git-inlay enable --repo --cwd .
```

Feedback labels the currently prepared candidate and retains only scoped hashes
and labels. Disable/enable/snooze apply globally when `--repo` is omitted. Local
learning has an independent `learning.enabled` preference. Config/keymap changes
take effect in new shells; preparation sees generation preferences on observe.

```sh
zsh-git-inlay doctor --fix
zsh-git-inlay undo list
zsh-git-inlay undo <printed-transaction-id>
zsh-git-inlay uninstall
```

Removal prints the retained private helper's exact undo command, so recovery
works after removing the installed executable from PATH. Run
`zsh_git_inlay_unload` in an already-open shell, or start a new one, to unload
its in-memory hooks/bindings. Default removal retains preferences and feedback.
`uninstall --purge-local-data` explicitly deletes product-owned local data and
backups; it validates dedicated product directories before recursive deletion.

Doctor preserves ambiguous/duplicate shell blocks, artifact symlinks, retargeted
installations, incompatible endpoints and later conflicting user edits for
manual review. It never grants data access. Unknown/malformed TOML expressions
are quarantined when they can be isolated; an ambiguous expression returns an
error without writing a blanket replacement. Full purge refuses ambiguous
custom override directories whose ownership boundary cannot be established.
Repository policy files remain part of their repositories.
