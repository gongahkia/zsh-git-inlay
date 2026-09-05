# Grounding and ranking

Providers propose a Conventional Commit candidate and evidence IDs. They do not
decide whether their proposal is supported. Before a daemon worker publishes a
candidate, deterministic code checks that every evidence ID exists in the
bounded staged context, the type is in the built-in Conventional Commit
allowlist, the subject is a single bounded line, any issue ID is present in the
bounded branch inference, and test claims have a staged test-like path.

The worker requires a scope to be `repo` or name a changed-path component, and
also compares subject components with changed paths. Fixes and claimed
behavioral consequences cannot be semantically proved from a patch by this
implementation. Those checks are explicitly marked heuristic and require
component overlap; otherwise the claim is unsupported.
This is evidence filtering, not a claim of complete program verification.

Each candidate receives one of these states:

| State | Meaning |
| --- | --- |
| `GROUNDED` | structural checks and available evidence passed without heuristic claims |
| `PARTIALLY_GROUNDED` | structural evidence passed, with one or more heuristic signals |
| `UNGROUNDED` | an unsupported reference, type, issue, subject, or claim was found |
| `INSUFFICIENT_CONTEXT` | no usable evidence was supplied |

Ranking prefers state, then evidence score (including recent repository subject
type as a heuristic style signal), then original provider order. The selected
policy is user-global only; repository configuration cannot change it:

```toml
[grounding]
ambiguity = "conservative" # conservative, quiet, visible, or hintable
```

`conservative` (the default) and `hintable` retain grounded and partially
grounded messages. If none remain, the explicitly configured deterministic
fallback may supply a conservative factual candidate. `quiet` retains only
grounded messages. `visible` retains structurally safe ungrounded messages but
still ranks them last. `zsh-autosuggestions` has no safe per-candidate visual
treatment, so `visible` is a ranking policy rather than a competing renderer;
`hintable` uses the same safe selection as conservative and is inspectable via
diagnostics.

Use the administrative diagnostic after candidates are ready:

```zsh
zsh-git-inlay explain --cwd .
zsh-git-inlay explain --cwd . --json
```

It reports candidate text already available through `candidates`, grounding
state, score, evidence IDs, and whether each check is deterministic or
heuristic. It does not print staged source or context payloads.
