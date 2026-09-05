# Privacy

`zsh-git-inlay` has no telemetry, account system, cross-device sync, automatic
model download, automatic staging, or automatic commit. The normal Zsh lookup
uses only the current exact Git staged-state identity and an owner-only local
socket lookup; it does not compile context, invoke a model, or contact a
network.

## Local data boundaries

Candidate records are private, bounded metadata under the XDG cache location.
They are keyed by repository/worktree identity and exact staged fingerprint.
Unstaged working-tree content is excluded by default. Context compilation is a
background operation and reads bounded staged Git inputs; it redacts common
secret forms and treats repository text as untrusted data. Redaction is
defense in depth, not a claim that arbitrary secrets are detectable.

An obsolete record can survive the narrow final-snapshot race until collection.
It cannot match a later exact fingerprint or scope, and retention is bounded by
the configured record count, byte limit, and maximum age (512 records, 32 MiB,
and seven days by default). Candidate records can still contain generated
metadata derived from staged paths, so users handling sensitive repositories
should choose tighter cache limits or remove the product cache explicitly.

Learning profiles live in private XDG data storage and contain bounded
aggregate style counts only—not commit text, bodies, source, activity, remote
URLs, credentials, candidates, or provider grants. See [LEARNING.md](LEARNING.md).
Evaluation reports default to private XDG state storage and intentionally omit
raw replay diffs.

The runtime socket is `0600` in a `0700` directory. Cache, state, and data
paths follow the XDG locations in [INSTALLATION.md](INSTALLATION.md); test and
troubleshooting overrides are documented there and in the runtime diagnostics.

## Optional activity

Activity is disabled unless the user runs `zsh-git-inlay permissions enable
activity`. A repository policy cannot enable it. Granted collection retains
only bounded, redacted, memory-only events for the configured TTL, scoped to
the repository/worktree. Command arguments, environment, standard input,
standard output, and standard error are not captured. Output capture is not
implemented and has no permission grant. Revoking activity immediately clears
memory and makes affected candidate records stale. See [ACTIVITY.md](ACTIVITY.md).

## Optional providers

The deterministic provider is the default. Ollama is loopback-only and is
explicitly selected; the software never pulls a model. OpenAI is default-deny
and can receive selected, redacted, relevance-filtered context categories only
after a user-private per-provider grant. Repository policy cannot grant it,
and revocation immediately prevents cloud-derived cache records from rendering.
No live OpenAI credential or request was used for this V1 validation. See
[CLOUD.md](CLOUD.md) and [PROVIDERS.md](PROVIDERS.md).

`compose` writes a user-visible owner-only temporary message file only after an
explicit command. It is not cached by the compose package, never commits, and
retains user edits as user-authored text. The command prints the file path so
the user controls later use and removal. See [COMPOSE.md](COMPOSE.md).

## Limits and remaining trust assumptions

Private Unix permissions reduce other-user access on supported systems but do
not protect a compromised user account. Git, the selected editor, local model
runtime, cloud provider after a grant, and the operating system remain separate
trust boundaries. The complete mitigations and test boundaries are in
[THREAT-MODEL.md](THREAT-MODEL.md). Live model/cloud behavior, broad hardware
compatibility, and third-party security review remain unverified.
