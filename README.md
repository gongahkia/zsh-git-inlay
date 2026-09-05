# zsh-git-inlay

`zsh-git-inlay` is a local-first, Zsh-only prototype that prepares deterministic Git commit-message candidates for the current staged state. It renders no text itself: a `git-inlay` strategy registered with the required [zsh-autosuggestions](https://github.com/zsh-users/zsh-autosuggestions) dependency supplies the normal dimmed ghost text.

```zsh
git commit -m 'chore(cache): update 2 staged files'
              # ^ prepared autosuggestion; accept with the usual forward-char/end-of-line binding
```

This is deliberately a proof of interaction, isolation, cache, and freshness properties. It makes no cloud request without an explicit per-provider user grant, and does not use a model download, telemetry, unconsented activity collection, or a second frontend.

## Install

Requirements are Git, Zsh 5.0.8 or later, Go 1.26 or later to build, and `zsh-autosuggestions` loaded before this plugin. On Fedora, the packaged autosuggestions source is normally `/usr/share/zsh-autosuggestions/zsh-autosuggestions.zsh`.

```zsh
git clone https://github.com/gongahkia/zsh-git-inlay.git
cd zsh-git-inlay
make build
install -Dm755 .build/zsh-git-inlay "$HOME/.local/bin/zsh-git-inlay"
```

Add the following in `.zshrc`, after the line that sources `zsh-autosuggestions`:

```zsh
path=("$HOME/.local/bin" $path)
source /absolute/path/to/zsh-git-inlay/zsh-git-inlay.plugin.zsh
```

The plugin prints an actionable diagnostic and stays inactive when autosuggestions has not been loaded. Check the local setup with:

```zsh
zsh-git-inlay doctor
```

On every prompt, the plugin starts a private per-user daemon lazily when needed and asks it to observe the current repository in the background. Typing `git commit -m ` only retrieves a candidate that is already fresh; it never waits for generation.

`^Xg` is the default emacs keymap binding to cycle prepared candidates. It wraps after the third candidate. Set `ZSH_GIT_INLAY_CYCLE_KEYBINDING=none` before sourcing the plugin to disable it, or use the XDG setting below. Cycling fetches only existing candidates and does not start the daemon or generate a candidate.

## Configuration

`$XDG_CONFIG_HOME/zsh-git-inlay/config.toml` (or `~/.config/zsh-git-inlay/config.toml`) is intentionally small:

```toml
[daemon]
idle_timeout = "15m"
max_active_repositories = 32
max_generation_concurrency = 2

[cache]
max_records = 512
max_bytes = 33554432
max_age = "168h"

[provider]
name = "deterministic"
# model = "qwen2.5-coder:0.5b" # required for name = "ollama" or "openai"
timeout = "8s"
fallback = "deterministic" # or "none"; openai requires "none"

[grounding]
ambiguity = "conservative" # conservative, quiet, visible, or hintable

[activity]
retention = "30m" # bounds memory only; it cannot enable collection
max_events = 256

[zsh]
cycle_keybinding = "^Xg"

[diagnostics]
verbose = false
```

Values are schema-validated; unknown keys are rejected. A repository may
optionally contain `.zsh-git-inlay.toml` with declarative `[commit]`
convention, type/scope, scope-path, length, capitalization, and body-preference
fields. Its content influences the fingerprint, but it never executes and
cannot configure a provider, activity collection, credentials, permissions, or
command hooks. See [repository policy](docs/POLICY.md).

Candidate storage is bounded by `cache.max_records`, `cache.max_bytes`, and
`cache.max_age`. The defaults retain at most 512 candidate records, 32 MiB, and
seven days. Startup and publication remove malformed, expired, and
over-capacity records; `zsh-git-inlay status --json` reports the resulting
storage and removal counters without printing candidate content.

`zsh-git-inlay evaluate --fixtures` runs the versioned public synthetic
evaluation corpus. `zsh-git-inlay evaluate --repo /path/to/local/repository`
replays eligible local parent-to-commit staged states into a temporary index.
Both emit JSON and Markdown reports to a private XDG state directory by default;
see [the evaluation guide](docs/EVALUATION.md). Evaluation is administrative
only and does not alter the Zsh suggestion path.

The default provider is deterministic. A locally installed Ollama model can be
selected explicitly; the plugin never pulls a model or falls back to cloud.
OpenAI is an explicitly selected, default-deny cloud adapter that requires a
private provider grant and `fallback = "none"`; it is not a local fallback.
See [provider configuration and validation status](docs/PROVIDERS.md) and the
[cloud grant guide](docs/CLOUD.md).

When Ollama is selected, the daemon compiles a bounded, staged-only context
before inference. It includes selected patch and repository evidence, redacts
common secret forms, treats repository text as untrusted data, and never runs
on the lookup path. Inspect the source choices and provider-specific budget
without printing source content with `zsh-git-inlay context --cwd . --provider
ollama`; see [the context compiler guide](docs/CONTEXT.md).

Activity context is disabled until the user grants it with `zsh-git-inlay
permissions enable activity`. It remains memory-only, repository/worktree
scoped, and feeds providers only bounded derived event-kind counts. With that
grant, safe Zsh hooks classify recognized command/test/build and Git transitions
without retaining command arguments or output. Transparent command-output
capture is intentionally unsupported. See [the local activity
protocol](docs/ACTIVITY.md).

Neovim 0.10+ can optionally emit bounded file and diagnostic-count activity
events; it never renders suggestions or reads buffer content. See [the Neovim
adapter guide](docs/NEOVIM.md).

Repository-local preference learning is enabled by default. It keeps only
bounded aggregate style counts from completed local commits, ranks only
already-grounded candidates, and has inspect/reset/disable/export/import
controls. See [the learning guide](docs/LEARNING.md).

Cloud preview and grant controls are administrative and never reveal source
content. A grant is a complete provider-specific replacement and requires
`--confirm`; revocation immediately makes cloud-derived cache records
unavailable. See [the cloud grant guide](docs/CLOUD.md).

`zsh-git-inlay compose` is a secondary, explicit editor workflow for an
already prepared candidate that passed subject/evidence checks. It produces a
private message file with a grounded staged-path body, rechecks the exact
staged state after editing, and never stages or commits. Normal ghost-text
suggestions remain subject-only; a required-body candidate is held for compose
instead of rendered. See [the compose guide](docs/COMPOSE.md).

The runtime socket defaults to `$XDG_RUNTIME_DIR/zsh-git-inlay/daemon.sock`; when that is unavailable, it uses a private XDG cache fallback. `ZSH_GIT_INLAY_RUNTIME_DIR` and `ZSH_GIT_INLAY_CACHE_DIR` are test and troubleshooting overrides.

## Diagnostics and development

`fingerprint`, `context`, `status`, `candidates`, `explain`, `permissions`,
`cloud`, `activity`, `learning`, and `daemon stop` are administrative commands,
not alternate commit-message workflows. `compose` is the separate explicit
secondary editor workflow described above:

```zsh
zsh-git-inlay fingerprint --cwd .
zsh-git-inlay context --cwd . --json
zsh-git-inlay context --cwd . --provider ollama
zsh-git-inlay status --json
zsh-git-inlay candidates --cwd .
zsh-git-inlay explain --cwd . --json
zsh-git-inlay permissions
zsh-git-inlay cloud preview --provider openai --cwd . --json
zsh-git-inlay compose --cwd . --candidate 0 --json
zsh-git-inlay activity inspect --cwd . --json
zsh-git-inlay learning inspect --cwd . --json
zsh-git-inlay suggest --cwd . --buffer 'git commit -m ' --json
zsh-git-inlay daemon stop
```

The JSON suggestion diagnostic distinguishes unsupported syntax, outside/no-staged/conflicted repositories, daemon startup/unavailability, pending/ready/stale cache results, malformed replies, and a user prefix that matches no candidate. The interactive strategy intentionally stays silent for those states.

Candidates are grounded and ranked in the daemon before publication. Provider
evidence references are checked against staged context; deterministic checks
are separated from heuristic fix/behavior signals. See [the grounding
guide](docs/GROUNDING.md) for policy behavior and `explain` output.

Build, test, lint, and benchmark the prototype with:

```zsh
make build
make test
make lint
make bench
```

The integration test uses a real Zsh process and the installed autosuggestions implementation to verify strategy registration, ghost-text strategy output, normal acceptance, cycling, stale rejection, no-staged behavior, dependency diagnostics, and absence of a `git()` override. See [docs/PROTOTYPE.md](docs/PROTOTYPE.md) for its renderer limitation and a manual PTY demonstration.

## Uninstall

Remove the source line from `.zshrc`, then in an active shell run `zsh_git_inlay_unload` before reloading the shell. Stop the daemon and remove only files you installed:

```zsh
zsh-git-inlay daemon stop
rm "$HOME/.local/bin/zsh-git-inlay"
# remove the cloned plugin directory if it is no longer needed
```

Optional cached candidates are under `$XDG_CACHE_HOME/zsh-git-inlay` (or `~/.cache/zsh-git-inlay`). Removing that directory is recoverable only from backups; it contains no repository content, only candidate metadata records.

Further design, security, and roadmap details are in [PRODUCT.md](docs/PRODUCT.md), [ARCHITECTURE.md](docs/ARCHITECTURE.md), [ACTIVITY.md](docs/ACTIVITY.md), [CLOUD.md](docs/CLOUD.md), [COMPOSE.md](docs/COMPOSE.md), [LEARNING.md](docs/LEARNING.md), [NEOVIM.md](docs/NEOVIM.md), and [THREAT-MODEL.md](docs/THREAT-MODEL.md).
