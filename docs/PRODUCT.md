# Product

## Implemented V1

`zsh-git-inlay` is one Zsh product. Its ordinary user interaction is unchanged Git syntax: type `git commit -m ` and let the required `zsh-autosuggestions` renderer show one prepared candidate. The user may accept with their ordinary autosuggestion binding, keep typing to constrain it, dismiss it, or press the configurable cycle binding to choose another already prepared candidate. There is no `git inlay` subcommand, Git wrapper, separate daemon product, editor frontend, menu, spinner, or TUI.

The Go executable's `doctor`, `status`, `fingerprint`, `context`, `candidates`,
`observe`, `evaluate`, `permissions`, `cloud`, `activity`, `learning`, and `daemon` commands are
setup, diagnostic, and test facilities only. `compose` is an explicit,
secondary editor utility in the same product; it is not an interactive
frontend or a commit executor. `evaluate` is an offline administrative harness,
not an alternative commit-message workflow. The
deterministic generator remains the default and test oracle. An explicitly
selected existing local Ollama runtime can generate structured candidates from
bounded staged-only context; mocked contracts and a live development evaluation
exist, but the three tested Qwen candidates were rejected on this host and no
Ollama model is recommended. An explicitly selected OpenAI adapter
also has mock-only contract validation; it is default-deny until a user grants
the exact cloud context classes and has no live-provider claim on this host.

## Product decisions

The following are implemented or settled product decisions:

- The product remains Zsh-only and `zsh-autosuggestions` remains required; the Go engine remains internal to that Zsh product.
- Inference remains local-first. A compatible existing Ollama installation may
  be reused. A future managed tiny local model requires a signed manifest and
  explicit consent; no such production distribution is configured. There is no
  silent local-to-cloud fallback.
- OpenAI requires an explicit, private, provider-specific replacement grant for
  selected versioned context classes, then current staged-state revalidation
  before each request. It uses no automatic fallback; revocation rejects cached
  cloud results before rendering. The adapter requests `store: false`, which is
  not a claim about all provider-side data handling.
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
- Normal flow continues to show one suggestion with cycling. Ambiguity defaults to a conservative factual message; users may choose conservative, quiet, visible, or hintable ambiguity behavior. In hintable mode, a typed message prefix reranks only matching already-prepared candidates in their existing safe order; it does not request generation, context compilation, daemon startup, or a question from ZLE.
- Local preference learning is enabled by default and scoped per repository.
  It retains bounded aggregate style signals from completed local commits and
  reranks only candidates that have already passed policy and grounding. It
  does not fine-tune a model. Linked worktrees share a profile, while staged
  caches remain isolated; matching-clone transfer requires explicit confirmation.
- Neovim is the sole V1 editor-context adapter. It is an opt-in event producer,
  never a suggestion frontend or independent inference caller; it emits bounded
  file and diagnostic-count transitions only.
- Optional commit bodies use `zsh-git-inlay compose`, a user-invoked secondary
  flow. It starts from an exact-fingerprint candidate that passed all
  subject/evidence checks; one withheld solely for a required-body policy is
  never rendered as ghost text. Compose proposes only staged path/status facts,
  opens the configured Git editor, rechecks state, and reports a private
  message-file path. It never commits or stages content; user edits remain
  user-authored rather than being relabeled as grounded generated facts.

VS Code, JetBrains, model fine-tuning, cross-device synchronization, dashboards,
and CI enforcement are deferred. This V1 does not implement a managed
model distribution, output capture, other editor/LSP
integrations,
issue trackers, automatic staging, automatic commits, standalone ghost text,
another shell, GUI/TUI, or telemetry.
