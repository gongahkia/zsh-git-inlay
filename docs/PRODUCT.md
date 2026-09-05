# Product

## Implemented prototype

`zsh-git-inlay` is one Zsh product. Its ordinary user interaction is unchanged Git syntax: type `git commit -m ` and let the required `zsh-autosuggestions` renderer show one prepared candidate. The user may accept with their ordinary autosuggestion binding, keep typing to constrain it, dismiss it, or press the configurable cycle binding to choose another already prepared candidate. There is no `git inlay` subcommand, Git wrapper, separate daemon product, editor frontend, menu, spinner, or TUI.

The Go executable's `doctor`, `status`, `fingerprint`, `context`, `candidates`,
`observe`, `evaluate`, and `daemon` commands are setup, diagnostic, and test
facilities only. `evaluate` is an offline administrative harness, not an
alternative commit-message workflow. The deterministic generator remains the
default and test oracle. An explicitly selected existing local Ollama runtime
can generate structured candidates from bounded staged-only context; it has
mocked contract validation but no live-model quality claim on this host.

## Settled future direction

The following are product decisions, not implemented features in this milestone:

- The product remains Zsh-only and `zsh-autosuggestions` remains required; the Go engine remains internal to that Zsh product.
- Inference remains local-first. A compatible existing Ollama installation may
  be reused. A future managed tiny local model requires a signed manifest and
  explicit consent; no such production distribution is configured. There is no
  silent local-to-cloud fallback.
- Cloud providers require explicit user capability grants and, after permission, may receive equivalent selected context.
- Activity collection is disabled by default. Retention will be user-configurable and memory-only by default. Commands and exit statuses require activity permission; bounded command output requires a separate permission.
- Relevance filtering and redaction occur before inference. The current
  compiler includes bounded staged patch/paths, declarations, root manifests,
  repository subjects, staged convention, branch, and issue evidence. Activity,
  tests, editor events, and LSP diagnostics remain future context sources.
- Repository configuration controls message convention and scopes only. It must never configure provider access, credentials, activity capture, output capture, or executable behavior.
- Normal flow continues to show one suggestion with cycling. Ambiguity defaults to a conservative factual message; users may choose conservative, quiet, visible, or hintable ambiguity behavior.
- Grounding will rank candidates without cluttering normal UX, with optional diagnostics that explain grounding.
- Local preference learning will be enabled by default and scoped per repository. It will adapt ranking and style rather than fine-tune a model. Matching clones will ask before sharing learned preferences.
- Neovim is the only planned V1 context adapter. It remains an event producer, never a suggestion frontend or independent inference caller.
- Optional commit bodies will use a secondary administrative composition flow later.

VS Code, JetBrains, model fine-tuning, cross-device synchronization, dashboards,
and CI enforcement are deferred. This prototype does not implement a managed
model distribution, cloud providers, activity collection, output capture,
editor/LSP integration, source grounding, preference learning, commit bodies,
issue trackers, automatic staging, automatic commits, standalone ghost text,
another shell, GUI/TUI, or telemetry.
