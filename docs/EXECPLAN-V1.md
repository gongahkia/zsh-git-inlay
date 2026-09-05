# V1 execution plan

## Baseline audit — 2026-09-05

Starting state for this V1 effort is `7ab8367` on `main`. `main` and
`origin/main` both resolve to that commit. This contradicts the prior handoff's
claim that nothing had been pushed: the local repository has no evidence of
who pushed it, so this plan records the discrepancy rather than attributing it
or rewriting history.

History is linear:

```text
8dc6d2a idea
  -> bc338c2 newinternalsofrtheZSHmodel
  -> 2864c51 addedtheidea
  -> 48bc3ae ripmybrutha
  -> 7ab8367 fix: fingerprint without mutating Git index
```

`master` was renamed to `main` before `bc338c2`; the reflog does not identify
an author or reason beyond the local rename. The commits `2864c51` and
`48bc3ae` are present in current history and are preserved unchanged. Their
messages are not descriptive, but cosmetic history rewrites are out of scope.

### Verified evidence

| Item | Evidence |
| --- | --- |
| Working tree | clean at audit start |
| Go/Git/Zsh | Go 1.26.7, Git 2.55.0, Zsh 5.9 |
| Neovim | 0.11.6 available |
| Ollama | unavailable; no model query or download attempted |
| `zsh-autosuggestions` | local source found and real-Zsh integration passed |
| Prototype test/lint | `make test` and `make lint` passed |
| Prototype benchmark | `make bench` passed; snapshot 5.38 ms/op, deterministic generation 1.21 ms/op, warm socket lookup 0.059 ms/op in this audit run |
| Doctor | `go run ./cmd/zsh-git-inlay doctor --json` found Git, Zsh and autosuggestions; daemon was absent as expected |

### Baseline gaps and risks

- Content-addressed candidate records have no retention quota, count limit, or
  garbage collection. The existing final-validation race can therefore retain
  unreachable records indefinitely.
- Exact lookup prevents a retained stale record from rendering, but diagnostics
  do not distinguish or account for obsolete storage.
- Cache records are not versioned beyond their fingerprint and cannot support
  later provider/context/policy provenance.
- The current config parser supports only prototype fields; it needs a typed,
  versioned extension without weakening the repository security floor.
- The deterministic provider has no context, grounding, policy, activity,
  learning, provider, cloud, or composition capability.
- The V1 claims in `docs/PRODUCT.md` are future tense. New documentation must
  continue to separate implementation, mocked validation, platform limitation,
  rejection, and plan.

## Architecture retained from the prototype

The Zsh plugin is the only frontend and uses the supported ordered
`zsh-autosuggestions` strategy API. A strategy parses a bounded buffer and
performs only an exact staged-state identity plus bounded Unix-socket lookup;
generation remains in the daemon. The daemon owns caching and background work.
Fingerprints include repository/worktree identity, HEAD/unborn state, exact
staged tree, generator version, and configuration version. A private alternate
index is used for `git write-tree`, so fingerprinting does not modify the real
Git index.

This boundary remains unchanged unless a measured failing gate requires a
change.

## Milestones and gates

| Milestone | Deliverable | Required validation |
| --- | --- | --- |
| 0. baseline hardening | bounded cache lifecycle, stale-record accounting/GC, expanded Git/IPC/crash/rapid-change coverage, performance budgets | focused cache tests, `make test`, `make lint`, `make bench`, soak target |
| 1. evaluation | synthetic versioned corpus, local-history replay, JSON and Markdown reports | deterministic CI fixture and private-corpus exclusion test |
| 2. providers | minimal provider contract, deterministic provider, Ollama implementation and mocks | malformed/cancellation/supersession tests; live Ollama only if available |
| 3. managed local model | consented acquisition design, pinned-manifest checks, install/rollback/uninstall diagnostics | mocked download/checksum/platform tests; document missing release-signing authority if applicable |
| 4. context | bounded staged-only compiler, redaction, inspection command and cache identity | deterministic budget/redaction/prompt-injection tests |
| 5. grounding | structural candidate schema, evidence validation, ranking and explain diagnostics | adversarial unsupported-claim tests and lookup regression benchmark |
| 6. policy | typed declarative repository convention/scope policy and provenance diagnostics | malicious configuration and precedence tests |
| 7–8. activity | versioned local event protocol, explicit permissions, safe Zsh hooks and separate output capture | consent/revocation/TTL/repository-isolation/secret-redaction tests |
| 9. Neovim | Lua event-only adapter and health checks | headless Neovim tests on this host; disclose other-platform gaps |
| 10. learning | bounded per-repository profile, controls, import/export and clone consent | reset/disable/isolation/consent tests |
| 11. cloud | capability-grant model and mock provider contract | no-transmission-without-grant, revocation, credential-redaction tests; no paid live request |
| 12. UX extensions | prefix-conditioned reranking and safe secondary compose flow | no-blocking/stale/never-commit tests |
| 13. release engineering | versioning, install/upgrade/uninstall, checksums, CI and release snapshot | clean-install lifecycle and reproducible snapshot test |
| 14. reliability | integration, evaluation, soak, security and performance report | all project targets, regression budget review |

## Performance policy

The prototype benchmark is noisy on this shared host, so V1 uses both
absolute limits and a relative investigation threshold. The synchronous ZLE
path must remain free of generation, repository patch scanning, daemon startup
waits, and network access. The asynchronous strategy helper must have a 50 ms
local lookup deadline. The current absolute benchmark limits are 1 ms for
parser composition, 0.25 ms for a warm local socket lookup, and 25 ms for the
background exact-state snapshot. A benchmark regression exceeding 25% from the
recorded baseline is investigated and recorded; it is accepted only with
measured evidence and an updated rationale. Provider/context/grounding work
must remain off the lookup path.

## Rejected approaches

- A Git wrapper, alternate shell frontend, standalone renderer, menu, TUI, or
  editor suggestion frontend: violate the single Zsh product boundary.
- `git write-tree` against the real index: it can update Git's cache-tree
  extension. The alternate private index copy is retained.
- Unbounded raw cache retention: fails the V1 privacy and storage gate.
- Automatic model downloads or cloud fallback: violate explicit consent.
- Transparent shell output interception: changes command semantics and is not
  an acceptable default collection mechanism.

## Progress log

- 2026-09-05: audited current state, history, remotes, code, docs, tests,
  benchmarks, doctor, and installed local runtimes. Milestone 0 began.
- 2026-09-05: added cache retention settings (record count, byte, and age
  bounds), deterministic startup/publication garbage collection, cache metrics,
  malformed-record validation, and a post-publication stale discard. Focused
  and full validation are recorded with the milestone commit.
- 2026-09-05: expanded Milestone 0 coverage for split indexes, submodule
  gitlinks, hostile filenames, truncated IPC, concurrent clients, rapid index
  changes, cache count/age/byte limits, and killed-daemon recovery. Five-run
  measurements on the shared host showed snapshot latency from 13.74 to 14.70
  ms/op, warm lookup from 0.138 to 0.152 ms/op, and parser composition at
  0.36 µs/op. These values meet the absolute limits and remain below the
  previously documented prototype measurements (23.13 ms snapshot and
  0.208 ms warm lookup), but exceed the 25% relative threshold from this
  audit's unusually fast first run. The unchanged parser measurement also
  varied substantially, so host scheduling is the strongest available
  explanation; [Inference] it is not evidence that the split-index support
  caused the observed change. The variation requires continued monitoring;
  generation and provider work remain outside the asynchronous lookup path.
- 2026-09-05: completed Milestone 1's versioned public synthetic corpus and
  deterministic CI runner. The harness records structural/evidence-token
  checks, cold/warm timing, approximate process allocation, failures, provider
  metadata, evaluator settings, and local hardware identifiers in JSON and
  Markdown reports. Historical replay reconstructs single-parent commits in a
  temporary staged index from local objects; reports stay in a private XDG
  state directory and intentionally omit raw diffs. String similarity to a
  historical subject is not scored. The corpus has fixture, replay,
  report-privacy, and timeout/error-accounting tests; model-provider wiring is
  deliberately deferred to Milestone 2.
- 2026-09-05: completed Milestone 2's minimal provider boundary: deterministic
  and loopback-only Ollama implementations, strict structured output, bounded
  metadata prompt/response sizes, cancellation, explicit deterministic-or-none
  fallback, cache provenance, provider-relevant configuration identity, and
  doctor diagnostics. Mock contract tests cover listing, inspection, shutdown,
  malformed/trailing output, cancellation, fallback, and supersession. Ollama
  is absent on this host, so live local-model validation and model comparison
  are explicitly unavailable; no download was attempted and deterministic
  remains the default.
- 2026-09-05: completed Milestone 3's secure managed-model acquisition design:
  private XDG data storage, HTTPS/pinned-checksum validation, resumable partial
  artifact handling, atomic installation, rollback, confirmation-gated
  uninstall, platform checks, and diagnostics are verified with fixtures. No
  authenticated runtime/model manifest or distribution-signing authority exists
  for this project, so the install command fails closed without any download.
  This is an external release-infrastructure blocker, not a completed managed
  runtime distribution.
- 2026-09-05: completed Milestone 4's inspectable context compiler. It uses
  bounded Git-index and metadata reads with stable path scoring/order; selected
  staged patch, symbols, root manifests, history, convention, branch, and issue
  sources have individual and provider-specific aggregate byte budgets. It
  excludes generated/vendor/lockfile content, redacts common secret forms, and
  frames every repository-derived field as untrusted data. The daemon compiles
  it before provider invocation, while the ZLE lookup path remains unchanged.
  Context identity includes the branch and compiler version in the candidate
  cache identity. Tests cover deterministic selection, budget enforcement,
  large repositories/diffs, prompt-injection framing, secret redaction, and
  unstaged-content exclusion. `context` and `context --json` show only source
  summaries, never context content. This validates compiler mechanics and
  bounds; it does not claim arbitrary-secret detection or live Ollama quality.
  Focused package tests and `go test ./...`, `make test`, and `make lint`
  passed. Two post-change benchmark runs recorded parser 0.303–0.472 µs/op,
  exact snapshot 9.58–14.88 ms/op, deterministic generation 1.74–2.74 ms/op,
  and warm socket lookup 0.104–0.175 ms/op. All remain within the documented
  1 ms/25 ms/0.25 ms absolute interactive budgets; the shared-host spread is
  retained as evidence rather than attributed to the compiler. `doctor --json`
  still found no running daemon or reachable Ollama and made no model request
  or download.
- 2026-09-05: completed Milestone 5's pre-publication grounding and ranking.
  Deterministic checks validate structured evidence IDs, conventional type and
  subject shape, context-derived scope, bounded branch issue references, and
  staged test-path evidence. Component/style matching and fix/behavior claims
  are explicitly heuristic; unsupported references and claims are rejected or
  demoted rather than treated as semantic proof. Candidate records retain
  grounding diagnostics, and `explain --json` is covered against a private test
  daemon. A validated user-global ambiguity policy supports conservative,
  quiet, visible, and hintable selection without a competing Zsh renderer.
  Focused race tests plus `go test ./...`, `make test`, and `make lint` passed.
  The benchmark recorded parser 0.228 µs/op, exact snapshot 6.69 ms/op,
  deterministic generation 1.19 ms/op, and warm socket lookup 0.073 ms/op,
  all within the existing absolute budgets. Doctor again found no daemon or
  reachable Ollama; no live model request or download occurred.
