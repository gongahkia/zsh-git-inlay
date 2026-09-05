# Product

## Implemented prototype

`zsh-git-inlay` is one Zsh product. Its ordinary user interaction is unchanged Git syntax: type `git commit -m ` and let the required `zsh-autosuggestions` renderer show one prepared candidate. The user may accept with their ordinary autosuggestion binding, keep typing to constrain it, dismiss it, or press the configurable cycle binding to choose another already prepared candidate. There is no `git inlay` subcommand, Git wrapper, separate daemon product, editor frontend, menu, spinner, or TUI.

The Go executable's `doctor`, `status`, `fingerprint`, `candidates`, `observe`, `evaluate`, and `daemon` commands are setup, diagnostic, and test facilities only. `evaluate` is an offline administrative harness, not an alternative commit-message workflow. The current generator is intentionally deterministic and prototype-quality; it proves preparation, caching, quoting, candidate ranking, stale rejection, repository/worktree isolation, and reproducible structural evaluation, not message quality.

## Settled future direction

The following are product decisions, not implemented features in this milestone:

- The product remains Zsh-only and `zsh-autosuggestions` remains required; the Go engine remains internal to that Zsh product.
- Inference remains local-first. A compatible existing Ollama installation may be reused; otherwise the product will ask once before downloading a managed tiny local model. There is no silent local-to-cloud fallback.
- Cloud providers require explicit user capability grants and, after permission, may receive equivalent selected context.
- Activity collection is disabled by default. Retention will be user-configurable and memory-only by default. Commands and exit statuses require activity permission; bounded command output requires a separate permission.
- Relevance filtering and redaction occur before inference. Future context may include recent commits, conventions, relevant source, manifests, symbols, branch or issue identifiers, commands, tests, editor events, and LSP diagnostics.
- Repository configuration controls message convention and scopes only. It must never configure provider access, credentials, activity capture, output capture, or executable behavior.
- Normal flow continues to show one suggestion with cycling. Ambiguity defaults to a conservative factual message; users may choose conservative, quiet, visible, or hintable ambiguity behavior.
- Grounding will rank candidates without cluttering normal UX, with optional diagnostics that explain grounding.
- Local preference learning will be enabled by default and scoped per repository. It will adapt ranking and style rather than fine-tune a model. Matching clones will ask before sharing learned preferences.
- Neovim is the only planned V1 context adapter. It remains an event producer, never a suggestion frontend or independent inference caller.
- Optional commit bodies will use a secondary administrative composition flow later.

VS Code, JetBrains, model fine-tuning, cross-device synchronization, dashboards, and CI enforcement are deferred. This prototype does not implement any of those decisions, nor real LLM inference, model downloads, Ollama integration, cloud providers, activity collection, output capture, editor/LSP integration, source grounding, preference learning, commit bodies, issue trackers, automatic staging, automatic commits, standalone ghost text, another shell, GUI/TUI, or telemetry.
