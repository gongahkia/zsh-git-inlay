# Product

## Implemented prototype

`zsh-git-inlay` is one Zsh product. Its ordinary user interaction is unchanged Git syntax: type `git commit -m ` and let the required `zsh-autosuggestions` renderer show one prepared candidate. The user may accept with their ordinary autosuggestion binding, keep typing to constrain it, dismiss it, or press the configurable cycle binding to choose another already prepared candidate. There is no `git inlay` subcommand, Git wrapper, separate daemon product, editor frontend, menu, spinner, or TUI.

The Go executable's `doctor`, `status`, `fingerprint`, `context`, `candidates`,
`observe`, `evaluate`, `permissions`, `activity`, `learning`, and `daemon` commands are
setup, diagnostic, and test facilities only. `evaluate` is an offline
administrative harness, not an alternative commit-message workflow. The
deterministic generator remains the default and test oracle. An explicitly
selected existing local Ollama runtime can generate structured candidates from
bounded staged-only context; it has mocked contract validation but no
live-model quality claim on this host.

## Product decisions

The following are implemented or settled product decisions:

- The product remains Zsh-only and `zsh-autosuggestions` remains required; the Go engine remains internal to that Zsh product.
- Inference remains local-first. A compatible existing Ollama installation may
  be reused. A future managed tiny local model requires a signed manifest and
  explicit consent; no such production distribution is configured. There is no
  silent local-to-cloud fallback.
- Cloud providers require explicit user capability grants and, after permission, may receive equivalent selected context.
- Activity collection is disabled by default and has a user-only grant,
  user-configurable memory-only retention, per-repository/worktree isolation,
  TTL, redaction, and bounded derived-signal context. Granted Zsh hooks record
  only allowlisted command classes, exit status, duration, and actual Git
  transitions; command arguments and output remain uncollected. Transparent
  output capture is rejected and any future explicit output path requires a
  separate permission.
- Relevance filtering and redaction occur before inference. The current
  compiler includes bounded staged patch/paths, declarations, root manifests,
  repository subjects, staged convention, branch, issue evidence, and only
  consented bounded activity kind/count signals. Test/build, Neovim file, and
  LSP diagnostic-count producers contribute no raw event data.
- Grounding validates provider evidence structurally and ranks supported
  candidates before normal rendering. It distinguishes deterministic checks from
  heuristic fix/behavior signals and intentionally does not promise semantic
  verification.
- Repository configuration controls declarative message convention, types,
  scopes, path inference, length, capitalization, and body preference only. It
  must never configure provider access, credentials, activity capture, output
  capture, or executable behavior.
- Normal flow continues to show one suggestion with cycling. Ambiguity defaults to a conservative factual message; users may choose conservative, quiet, visible, or hintable ambiguity behavior.
- Local preference learning is enabled by default and scoped per repository.
  It retains bounded aggregate style signals from completed local commits and
  reranks only candidates that have already passed policy and grounding. It
  does not fine-tune a model. Linked worktrees share a profile, while staged
  caches remain isolated; matching-clone transfer requires explicit confirmation.
- Neovim is the sole V1 editor-context adapter. It is an opt-in event producer,
  never a suggestion frontend or independent inference caller; it emits bounded
  file and diagnostic-count transitions only.
- Optional commit bodies will use a secondary administrative composition flow later.

VS Code, JetBrains, model fine-tuning, cross-device synchronization, dashboards,
and CI enforcement are deferred. This prototype does not implement a managed
model distribution, cloud providers, output capture, other editor/LSP
integrations, commit bodies,
issue trackers, automatic staging, automatic commits, standalone ghost text,
another shell, GUI/TUI, or telemetry.
