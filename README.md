# zsh-git-inlay

`zsh-git-inlay` prepares Git commit-message candidates from the current staged
state and exposes them as ordinary
[zsh-autosuggestions](https://github.com/zsh-users/zsh-autosuggestions) ghost
text:

```zsh
git commit -m 'chore(cache): update 2 staged files'
              # ^ prepared autosuggestion; accept with your normal
              #   forward-character or end-of-line binding
```

It is a Zsh-only autosuggestion strategy, not a `git` wrapper, prompt theme,
TUI, or editor frontend. The plugin registers a `git-inlay` strategy and
leaves rendering to autosuggestions. A private local daemon prepares candidates
after each prompt; while typing, the strategy only looks up a candidate already
prepared for the exact staged state. It does not wait for generation.

The default provider is deterministic and local. Ollama and OpenAI are explicit
opt-in providers; neither downloads a model nor contacts a network by default.
There is no telemetry, automatic staging, automatic commit, account, or
cross-device sync.

## Table of Contents

- [Getting Started](#getting-started)
  - [System Compatibility](#system-compatibility)
  - [Prerequisites](#prerequisites)
  - [Basic Installation](#basic-installation)
  - [Manual Inspection](#manual-inspection)
- [Using zsh-git-inlay](#using-zsh-git-inlay)
  - [Prepared Suggestions](#prepared-suggestions)
  - [Configuration](#configuration)
  - [Diagnostics](#diagnostics)
  - [FAQ](#faq)
- [Advanced Topics](#advanced-topics)
  - [Advanced Installation](#advanced-installation)
  - [Repository Message Policy](#repository-message-policy)
  - [Providers and Context](#providers-and-context)
  - [Activity and Neovim](#activity-and-neovim)
  - [Local Preference Learning](#local-preference-learning)
  - [Commit-body Composition](#commit-body-composition)
  - [Privacy, Storage, and Freshness](#privacy-storage-and-freshness)
  - [Evaluation and Reliability](#evaluation-and-reliability)
- [Getting Updates](#getting-updates)
- [Uninstalling zsh-git-inlay](#uninstalling-zsh-git-inlay)
- [Contributing and Development](#contributing-and-development)
- [Project Status and License](#project-status-and-license)

## Getting Started

### System Compatibility

The current evidence is intentionally narrower than a general compatibility
claim:

| Environment | Status |
| --- | --- |
| Fedora Linux 43 | Source/runtime, Zsh integration, installation, uninstall, and release-snapshot checks were exercised locally. |
| Linux `amd64` and `arm64` | Release snapshots cross-compile for both; only the Fedora runtime path has been executed. |
| macOS `amd64` and `arm64` | Release snapshots cross-compile for both; no macOS runtime execution has been performed. |
| Other operating systems | Not verified. |

The plugin requires Zsh plus `zsh-autosuggestions`. It does not support Bash,
Fish, PowerShell, or a standalone terminal renderer.

### Prerequisites

You need:

- Git;
- Zsh 5.0.8 or later;
- [zsh-autosuggestions](https://github.com/zsh-users/zsh-autosuggestions),
  sourced before this plugin; and
- Go 1.26 or later to build the source checkout.

On Fedora, install the documented runtime prerequisites with:

```zsh
sudo dnf install git zsh zsh-autosuggestions golang
```

The Fedora autosuggestions package normally provides
`/usr/share/zsh-autosuggestions/zsh-autosuggestions.zsh`. Ollama, an OpenAI
API key, and Neovim are optional; none is needed for the deterministic default.

### Basic Installation

The supported install is a reviewed source checkout. The installer does not
download a model or edit `.zshrc`.

```zsh
git clone https://github.com/gongahkia/zsh-git-inlay.git
cd zsh-git-inlay
make build
sh scripts/install.sh --source "$PWD"
```

The default prefix is `$HOME/.local`. Add these lines after the line that
sources `zsh-autosuggestions` in `.zshrc`:

```zsh
path=("$HOME/.local/bin" $path)
source "$HOME/.local/share/zsh-git-inlay/zsh-git-inlay.plugin.zsh"
```

Start a fresh Zsh session, then check installation and load order:

```zsh
zsh-git-inlay version --json
zsh-git-inlay doctor --json
```

If autosuggestions was not loaded first, the plugin prints an actionable
diagnostic and remains inactive. It does not configure the dependency for you.

### Manual Inspection

There is no one-line remote installer. Inspect the checkout, build recipe, and
installer before running it:

```zsh
git clone https://github.com/gongahkia/zsh-git-inlay.git
cd zsh-git-inlay
sed -n '1,240p' Makefile
sed -n '1,260p' scripts/install.sh
sed -n '1,260p' zsh-git-inlay.plugin.zsh
make build
sh scripts/install.sh --source "$PWD"
```

The source installer atomically replaces only its executable and plugin under
the selected prefix. See [advanced installation](#advanced-installation) for a
custom prefix and the reviewed archive flow.

## Using zsh-git-inlay

### Prepared Suggestions

On every prompt, the plugin asks the local daemon to observe the current
repository in the background. Once the current staged state has a fresh
candidate, place the cursor after the space in this supported command form:

```zsh
git commit -m
```

Accept, dismiss, or continue typing with your existing autosuggestions bindings.
A typed message prefix narrows existing prepared candidates; it does not
synchronously generate another. Unsupported commands, no staged changes,
conflicts, a stale candidate, daemon startup, and unavailable optional providers
stay silent in the interactive strategy.

Only conservative `git commit` forms such as `git commit -m` and
`command git commit -m` are accepted. The parser rejects shell operators and
substitutions, completed message arguments, `--amend`, `--fixup`, `--squash`,
other Git commands, and a cursor that is not at the end of the buffer.

#### Cycling Prepared Candidates

`^Xg` is the default emacs-keymap binding for cycling prepared candidates. It
wraps after the available candidates and does not start a daemon or request
generation. The first prompt after normal stage/unstage work returns to the
primary candidate.

Set a persistent binding in configuration, or disable cycling for one shell
before sourcing the plugin:

```zsh
ZSH_GIT_INLAY_CYCLE_KEYBINDING=none
source "$HOME/.local/share/zsh-git-inlay/zsh-git-inlay.plugin.zsh"
```

### Configuration

Global settings are declarative TOML at
`$XDG_CONFIG_HOME/zsh-git-inlay/config.toml`, falling back to
`$HOME/.config/zsh-git-inlay/config.toml`. Unknown keys and malformed values
are rejected; configuration is data, not shell code.

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
name = "deterministic" # deterministic, ollama, or openai
# model = "qwen2.5-coder:0.5b" # required for ollama or openai
timeout = "8s"
fallback = "deterministic" # deterministic or none; openai requires none

[grounding]
ambiguity = "conservative" # conservative, quiet, visible, or hintable

[activity]
retention = "30m" # bounds memory; it does not grant collection
max_events = 256

[zsh]
cycle_keybinding = "^Xg"

[diagnostics]
verbose = false
```

Provider and context-relevant changes make old candidates ineligible for the
new configuration. [CONFIGURATION.md](docs/CONFIGURATION.md) documents all
bounds, defaults, and precedence.

### Diagnostics

The executable supplies setup and inspection commands; they are not another
commit-message interface.

```zsh
zsh-git-inlay version --json
zsh-git-inlay doctor --json
zsh-git-inlay status --json
zsh-git-inlay fingerprint --cwd .
zsh-git-inlay context --cwd . --provider ollama
zsh-git-inlay candidates --cwd .
zsh-git-inlay explain --cwd . --json
zsh-git-inlay suggest --cwd . --buffer 'git commit -m ' --json
zsh-git-inlay daemon stop
```

`context` reports source categories, byte budgets, fingerprints, truncation,
and redaction counts without printing compiled repository content. `explain`
reports candidate policy, grounding, and bounded learning adjustments.
`suggest --json` distinguishes unsupported commands, repository/daemon states,
stale or pending cache results, malformed replies, and unmatched prefixes.

### FAQ

#### Why is there no ghost text?

A cold repository, new staged state, or daemon restart has no candidate yet.
Return to a prompt after staging changes, then type a supported
`git commit -m` prefix. Use `zsh-git-inlay status --json` and
`zsh-git-inlay candidates --cwd .` to distinguish a pending candidate from an
unsupported or stale state.

#### Does it change Git or create a commit?

No. The plugin does not define `git` or invoke `git commit`. It gives
autosuggestions a shell-safe continuation; accepting it still leaves the normal
Git command under your control.

#### Why is the plugin inactive?

`zsh-autosuggestions` must be sourced before
`zsh-git-inlay.plugin.zsh`. Source it first, start a new shell, and use
`zsh-git-inlay doctor --json` for dependency and runtime diagnostics.

#### Does normal use send repository content to a service?

No. The deterministic default is local, and the normal lookup does not compile
context or call a provider. Ollama must be selected explicitly and is
loopback-only. OpenAI also needs a configured API key and an explicit,
provider-specific grant. See [providers and context](#providers-and-context).

## Advanced Topics

### Advanced Installation

#### Custom Prefix

Build the checked-out revision and choose a non-root prefix. The installer
creates or replaces only `<prefix>/bin/zsh-git-inlay` and
`<prefix>/share/zsh-git-inlay/zsh-git-inlay.plugin.zsh`.

```zsh
make build
sh scripts/install.sh --source "$PWD" --prefix /chosen/prefix
```

Then load it after autosuggestions:

```zsh
path=("/chosen/prefix/bin" $path)
source "/chosen/prefix/share/zsh-git-inlay/zsh-git-inlay.plugin.zsh"
```

#### Installing a Reviewed Release Archive

The snapshot builder creates Linux and macOS `amd64`/`arm64` archives
containing the executable, plugin, README, and changelog. After independently
verifying the published checksum, install an archive without a source checkout:

```zsh
sha256sum -c checksums.txt
sh scripts/install-release.sh \\
  --archive zsh-git-inlay_0.1.0-rc.1_linux-amd64.tar.gz \\
  --prefix "$HOME/.local"
```

The archive installer accepts only expected members and atomically installs the
same two product files. It does not modify shell configuration, cache, state,
data, or managed-model data. These are validation snapshots, not published
releases, while the repository has no license.

#### Manual Plugin Loading

For development, load a built checkout directly after autosuggestions:

```zsh
path=("$PWD/.build" $path)
source "$PWD/zsh-git-inlay.plugin.zsh"
```

Run `make build` first. Run `zsh_git_inlay_unload` to remove the plugin from
the current shell without restarting it.

### Repository Message Policy

A repository may include a strict declarative `.zsh-git-inlay.toml` file that
constrains a Conventional Commit convention, allowed types/scopes,
path-to-scope mappings, line length, capitalization, and body preference:

```toml
[commit]
convention = "conventional"
types = ["feat", "fix", "test", "docs"]
scopes = ["api", "cli"]
scope_paths = ["internal/api=api", "cmd=cli"]
line_length = 72
capitalization = "lower" # or "sentence"
body = "optional"         # forbid, optional, or required
```

Malformed or unknown policy entries make the state unsupported instead of being
partially applied. Policy cannot select a provider or endpoint, read
credentials, grant cloud access or activity collection, weaken redaction, or
execute a command. It participates in the exact candidate identity. See
[POLICY.md](docs/POLICY.md) for its grammar and fixed precedence.

### Providers and Context

Before an optional provider runs, a background job compiles bounded staged-only
context: selected paths and patch, nearby declarations, root manifests, bounded
history and branch evidence, plus consented derived activity signals. It excludes
working-tree content and applies common secret-pattern redaction and
untrusted-data framing. Redaction is defense in depth, not a guarantee that
every secret form is detectable.

#### Deterministic Provider

`deterministic` is the default. It needs no model runtime or network request
and remains the test oracle. It creates ordered conservative Conventional Commit
candidates from bounded staged Git metadata.

#### Ollama Provider

Choose an already installed local model explicitly:

```toml
[provider]
name = "ollama"
model = "qwen2.5-coder:0.5b"
timeout = "8s"
fallback = "deterministic" # or "none"
```

The adapter accepts only `http://127.0.0.1:11434`, never downloads or pulls a
model, and treats output as untrusted structured data. `doctor --json` can
inspect the loopback runtime and locally installed model names without running
or downloading one. Three Qwen models tested through the frozen local
evaluation gate produced no prepared candidates on the documented Fedora host,
so deterministic remains the default. That result is not a general
model-quality claim; see [PROVIDERS.md](docs/PROVIDERS.md) and
[EVALUATION.md](docs/EVALUATION.md).

#### OpenAI Provider

OpenAI is default-deny and has no automatic fallback. Select it globally, set
the key only in the environment, preview bounded categories, then replace the
complete context-class grant explicitly:

```toml
[provider]
name = "openai"
model = "gpt-5"
timeout = "8s"
fallback = "none"
```

```zsh
export OPENAI_API_KEY='...'
zsh-git-inlay cloud preview --provider openai --cwd . --json
zsh-git-inlay cloud grant openai \\
  --classes staged_diff,repository_context --confirm
```

The adapter reads `OPENAI_API_KEY` only at request time, uses a non-streaming
structured request with `store: false`, and rechecks the grant and exact staged
state before each HTTP attempt. `store: false` is a request setting, not a
statement about all provider-side data handling. Review current provider
controls before granting context. Revoke access with:

```zsh
zsh-git-inlay cloud revoke openai
```

Revocation removes that provider’s cached records and makes later lookup fail
closed. No live OpenAI credential or request was used for V1 validation; live
compatibility, billing, availability, and model quality remain unverified. Read
[CLOUD.md](docs/CLOUD.md) for grant classes and the request lifecycle.

### Activity and Neovim

Activity is disabled by default. Configuration can bound memory retention, but
only an explicit user command can grant it:

```zsh
zsh-git-inlay permissions
zsh-git-inlay permissions enable activity
zsh-git-inlay permissions revoke activity
```

With the grant, asynchronous Zsh hooks classify a small allowlist of Git, test,
and build events. They retain a recognized class, exit status, and duration;
they do not retain command arguments, environment, standard input, standard
output, or standard error. Events are repository/worktree scoped, bounded, and
memory-only. Transparent command-output capture is unsupported.

Neovim 0.10+ is an optional activity producer, not another suggestion frontend.
After adding this source checkout to Neovim’s runtime path and granting activity:

```lua
require("zsh-git-inlay").setup({
  file_events = true,
  diagnostics = true,
})
```

It emits a buffer path, session ID, and capped diagnostic severity counts; it
does not read buffer content, call providers, or render suggestions. Installers
do not install the Lua adapter. See [ACTIVITY.md](docs/ACTIVITY.md) and
[NEOVIM.md](docs/NEOVIM.md).

### Local Preference Learning

Local preference learning is enabled by default per repository identity. It
records bounded aggregate style counts from completed local commits, then
reranks only candidates that already passed policy and grounding. It does not
fine-tune a model, create a frontend, or treat displayed text as accepted.

```zsh
zsh-git-inlay learning status --cwd .
zsh-git-inlay learning inspect --cwd . --json
zsh-git-inlay learning disable --cwd .
zsh-git-inlay learning reset --cwd .
zsh-git-inlay learning export --cwd . > profile.json
zsh-git-inlay learning import --cwd . --file profile.json
```

Profiles contain aggregate type/scope, subject-shape, and limited verb
statistics—not commit subjects/bodies, source, diffs, activity, remotes,
credentials, candidates, or provider grants. Linked worktrees share a profile;
candidate caches stay worktree-specific. Transfer from a known matching clone
requires `--confirm`. See [LEARNING.md](docs/LEARNING.md).

### Commit-body Composition

Normal ghost text is a single-line subject. `zsh-git-inlay compose` is a
separate explicit editor workflow for a prepared, grounded candidate:

```zsh
zsh-git-inlay compose --cwd .
zsh-git-inlay compose --cwd . --candidate 1 --json
```

It proposes a subject plus a grounded staged-path body, opens the configured Git
editor, then rechecks the exact staged fingerprint. It reports an owner-only
message-file path; it neither stages content nor invokes `git commit`. If state
or policy changes during editing, the edited file remains available and is not
silently used.

A repository policy that requires a body holds an eligible prepared subject for
compose instead of rendering it as ghost text. A policy that forbids bodies also
forbids this workflow. See [COMPOSE.md](docs/COMPOSE.md).

### Privacy, Storage, and Freshness

Normal lookup uses the current exact staged-state identity plus a short bounded
owner-only Unix-socket lookup. It does not compile context, invoke a provider,
or contact a network. The daemon normally uses
`$XDG_RUNTIME_DIR/zsh-git-inlay/daemon.sock`, a `0600` socket inside a
`0700` directory, with a private XDG cache fallback.

Candidate records are private bounded metadata below the XDG cache location.
Default collection retains at most 512 records, 32 MiB, and seven days. Records
are keyed by repository/worktree identity and a fingerprint of the index,
HEAD/unborn state, branch, relevant configuration, and context identity.
Unstaged working-tree content is excluded.

A final publication race can leave an obsolete record until collection. It
cannot match a later exact fingerprint and scope, so it cannot render for the
new staged state; retention remains bounded. This is bounded stale storage, not
a claim that the race is eliminated.

The cache can contain generated metadata derived from staged paths. Tighten its
limits or remove product cache data for sensitive repositories. See
[PRIVACY.md](docs/PRIVACY.md), [CONTEXT.md](docs/CONTEXT.md), and
[ARCHITECTURE.md](docs/ARCHITECTURE.md).

### Evaluation and Reliability

Run the versioned synthetic corpus or replay eligible local history through a
temporary staged index:

```zsh
zsh-git-inlay evaluate --fixtures --json
zsh-git-inlay evaluate --fixtures --partition held-out --json
zsh-git-inlay evaluate --repo /absolute/path/to/repository --limit 50 --json
```

Reports are private JSON and Markdown files under the XDG state directory by
default. The evaluator does not replace foreground daemon, socket, or terminal
validation, and local-history reports do not include raw replay diffs.

The project checks include:

```zsh
make build
make test
make lint
make bench
make fuzz FUZZ_TIME=3s
make test-soak
make test-install
```

`make test` includes Go tests, real-Zsh integration, synthetic evaluation,
installation lifecycle checks, installed-plugin dogfooding, and a bounded
multi-shell soak. Neovim validation runs when available and reports an explicit
skip otherwise. [RELIABILITY.md](docs/RELIABILITY.md) describes the covered
scenarios and limitations; [EVALUATION.md](docs/EVALUATION.md) records the
frozen local-model protocol and results.

## Getting Updates

There is no automatic updater. Inspect the desired source revision, rebuild it,
and rerun the installer. It replaces only the installed executable and plugin;
it does not touch `.zshrc`, configuration, cache, state, activity, learning
profiles, or models.

```zsh
cd /path/to/zsh-git-inlay
git pull
make build
sh scripts/install.sh --source "$PWD" --prefix "$HOME/.local"
zsh-git-inlay version --json
```

Use a reviewed archive only after independently verifying its checksum. The
snapshot procedure and its verification boundary are in
[INSTALLATION.md](docs/INSTALLATION.md).

## Uninstalling zsh-git-inlay

Remove the plugin `source` line from `.zshrc`. In a running shell, call
`zsh_git_inlay_unload` before reloading Zsh. Then remove the installed product
files and stop the installed daemon when possible:

```zsh
sh scripts/uninstall.sh --prefix "$HOME/.local"
```

Configuration, cache, state, data, learning profiles, and runtime directories
are retained by default. Make removal of the product-named standard XDG
directories explicit:

```zsh
sh scripts/uninstall.sh --prefix "$HOME/.local" --purge-local-data
```

Custom `ZSH_GIT_INLAY_*` directory overrides are never guessed or removed.
The retained cache may contain candidate metadata; delete it only if you no
longer need it or have a backup. [INSTALLATION.md](docs/INSTALLATION.md)
documents exact paths and behavior.

## Contributing and Development

The repository contains a Go daemon, a small Zsh strategy plugin, and an
optional Neovim activity emitter. Start with these design documents before
changing the interaction boundary:

- [PRODUCT.md](docs/PRODUCT.md) defines product scope.
- [ARCHITECTURE.md](docs/ARCHITECTURE.md) explains the strategy, fingerprint,
  daemon, cache, IPC, provider, and grounding boundaries.
- [THREAT-MODEL.md](docs/THREAT-MODEL.md) and [PRIVACY.md](docs/PRIVACY.md)
  define privacy and security constraints.
- [RELIABILITY.md](docs/RELIABILITY.md) records verification scope.

Run the narrowest relevant check first, then broader checks. `make lint` runs
Go vet, verifies Go formatting, syntax-checks Zsh and shell scripts, and rejects
`eval` or a `git()` override in product paths. `make test-live-ollama` is
deliberately opt-in: it needs a separately started loopback runtime and an
already downloaded model named by `ZSH_GIT_INLAY_LIVE_OLLAMA_MODEL`.

## Project Status and License

This is an RC1-quality V1 implementation with source installation, integration,
evaluation, reliability, archive, checksum, and SBOM preparation in the
repository. The deterministic provider is the verified default. The documented
Ollama models did not pass the local evaluation gate, while OpenAI has mock
contract coverage only.

The repository has no `LICENSE` file and no license has been selected. Snapshot
generation is useful for internal validation, but publishing or redistributing
artifacts remains blocked until the maintainer adds a license. Read
[LICENSE-REVIEW.md](docs/LICENSE-REVIEW.md) and [CHANGELOG.md](CHANGELOG.md)
before treating a snapshot as a release.
