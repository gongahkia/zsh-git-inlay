# zsh-git-inlay

`zsh-git-inlay` is a local-first, Zsh-only prototype that prepares deterministic Git commit-message candidates for the current staged state. It renders no text itself: a `git-inlay` strategy registered with the required [zsh-autosuggestions](https://github.com/zsh-users/zsh-autosuggestions) dependency supplies the normal dimmed ghost text.

```zsh
git commit -m 'chore(cache): update 2 staged files'
              # ^ prepared autosuggestion; accept with the usual forward-char/end-of-line binding
```

This is deliberately a proof of interaction, isolation, cache, and freshness properties. It does not use an LLM, network, model download, telemetry, activity collection, or a second frontend.

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
# model = "qwen2.5-coder:0.5b" # required only for name = "ollama"
timeout = "8s"
fallback = "deterministic" # or "none"

[zsh]
cycle_keybinding = "^Xg"

[diagnostics]
verbose = false
```

Values are schema-validated; unknown keys are rejected. A repository may optionally contain `.zsh-git-inlay.toml` with only `[commit]` `convention`, `types`, `scopes`, and `line_length` keys. Its content influences the fingerprint, but it never executes and cannot configure a provider, activity collection, credentials, permissions, or command hooks.

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
selected explicitly; the plugin never pulls a model and never falls back to a
cloud service. See [provider configuration and validation status](docs/PROVIDERS.md).

When Ollama is selected, the daemon compiles a bounded, staged-only context
before inference. It includes selected patch and repository evidence, redacts
common secret forms, treats repository text as untrusted data, and never runs
on the lookup path. Inspect the source choices and provider-specific budget
without printing source content with `zsh-git-inlay context --cwd . --provider
ollama`; see [the context compiler guide](docs/CONTEXT.md).

The runtime socket defaults to `$XDG_RUNTIME_DIR/zsh-git-inlay/daemon.sock`; when that is unavailable, it uses a private XDG cache fallback. `ZSH_GIT_INLAY_RUNTIME_DIR` and `ZSH_GIT_INLAY_CACHE_DIR` are test and troubleshooting overrides.

## Diagnostics and development

`fingerprint`, `context`, `status`, `candidates`, and `daemon stop` are administrative commands, not alternate commit-message workflows:

```zsh
zsh-git-inlay fingerprint --cwd .
zsh-git-inlay context --cwd . --json
zsh-git-inlay context --cwd . --provider ollama
zsh-git-inlay status --json
zsh-git-inlay candidates --cwd .
zsh-git-inlay suggest --cwd . --buffer 'git commit -m ' --json
zsh-git-inlay daemon stop
```

The JSON suggestion diagnostic distinguishes unsupported syntax, outside/no-staged/conflicted repositories, daemon startup/unavailability, pending/ready/stale cache results, malformed replies, and a user prefix that matches no candidate. The interactive strategy intentionally stays silent for those states.

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

Further design, security, and roadmap details are in [PRODUCT.md](docs/PRODUCT.md), [ARCHITECTURE.md](docs/ARCHITECTURE.md), and [THREAT-MODEL.md](docs/THREAT-MODEL.md).
