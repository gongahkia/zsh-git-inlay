# Configuration

Global user settings live at
`$XDG_CONFIG_HOME/zsh-git-inlay/config.toml` (defaulting to
`~/.config/zsh-git-inlay/config.toml`). `ZSH_GIT_INLAY_CONFIG` is a test and
troubleshooting override. Unknown keys and malformed values fail loading;
configuration is parsed as data and never executed.

```toml
[daemon]
idle_timeout = "15m"              # positive duration
max_active_repositories = 32       # 1 through 256
max_generation_concurrency = 2     # 1 through 16

[cache]
max_records = 512                  # 1 through 4096
max_bytes = 33554432               # 65536 through 1073741824
max_age = "168h"                  # positive duration, at most 8760h

[provider]
name = "deterministic"            # deterministic, ollama, or openai
# model = "qwen2.5-coder:0.5b"    # required for ollama or openai
timeout = "8s"                    # 1s through 1m
fallback = "deterministic"        # deterministic or none; openai requires none

[grounding]
ambiguity = "conservative"        # conservative, quiet, visible, or hintable

[activity]
retention = "30m"                 # 1m through 24h; does not grant collection
max_events = 256                   # 1 through 4096 per repository/worktree

[zsh]
cycle_keybinding = "^Xg"

[diagnostics]
verbose = false
```

Changing valid global provider/context-relevant settings changes the exact
candidate identity; a candidate prepared under the old settings cannot serve
the new state. `zsh-git-inlay doctor --json` reports runtime and dependency
diagnostics without printing staged source content. `zsh-git-inlay config
cycle-keybinding` prints the configured cycle binding.

## Repository message policy

A repository may contain `.zsh-git-inlay.toml` with only the declarative
`[commit]` keys documented in [POLICY.md](POLICY.md): convention, type/scope
allowlists, path-to-scope mappings, line length, capitalization, and body
preference. It is part of the exact fingerprint and is strict—unknown or
malformed entries produce an unsupported state instead of partial application.

Repository policy cannot select a provider or endpoint, read credentials, grant
cloud classes, enable activity or output capture, weaken redaction, run a
command, or alter shell parsing. The fixed precedence is:

```text
built-in message and shell-safety floor
  -> user-global provider and privacy grants
  -> repository message convention and scopes
  -> safe invocation quoting and typed-prefix constraints
```

Cloud grants use the separate user-only commands in [CLOUD.md](CLOUD.md), and
activity uses the separate user-only commands in [ACTIVITY.md](ACTIVITY.md).
Neither is a TOML setting. The default deterministic provider remains local and
available when optional providers are absent; OpenAI has no fallback.
