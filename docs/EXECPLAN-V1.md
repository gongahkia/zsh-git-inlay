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
- 2026-09-05: completed Milestone 6's typed repository message policy. Strict
  declarative configuration now supports conventional type/scope allowlists,
  bounded relative path-to-scope inference, total line length, capitalization,
  and body preference. Grounding applies policy beneath the built-in safety
  floor and user-global privacy/provider controls; cache diagnostics expose the
  effective policy and provenance through `explain`. Unknown, capability-
  bearing, malformed, unsafe-path, duplicate, and inconsistent policy entries
  are rejected without partial application. Required bodies intentionally
  suppress subject-only suggestions until a future secondary compose flow can
  satisfy them. Focused race tests plus `go test ./...`, `make test`, and
  `make lint` passed. The benchmark recorded parser 0.462 µs/op, exact
  snapshot 19.19 ms/op, deterministic generation 2.79 ms/op, and warm socket
  lookup 0.145 ms/op, within the 1 ms/25 ms/0.25 ms absolute budgets. Doctor
  still found no running daemon or reachable Ollama; no model request or
  download occurred.
- 2026-09-05: completed Milestone 7's typed local activity protocol. Activity
  is default-deny behind a user-private `0600` permission record; the daemon
  reloads that record for each event and signal read, clears memory on
  revocation, and rejects non-private, malformed, or unknown permission
  records. Version-1 events have fixed source/kind allowlists, SHA-256
  repository/worktree IDs, bounded timestamps and structured data, sensitivity
  classification, pre-retention redaction, TTL, per-scope and global memory
  bounds, and replay fixtures. The private owner-only socket authenticates an
  event's claimed scope against a fresh local Git snapshot. `permissions` and
  `activity inspect|clear` provide the requested user controls. Only up to ten
  allowlisted kind/count signals (512 bytes) can reach compiler context; raw
  event data never does. Candidate records retain only that derived
  representation/digest, and changed, cleared, revoked, or expired signals
  make lookup `activity_stale`. Focused race tests for activity, config, IPC,
  context, daemon, and CLI passed, as did `go test ./...`, `make test`, and
  `make lint`. Benchmark results were parser 0.299 us/op, exact snapshot 8.37
  ms/op, deterministic generation 1.52 ms/op, and warm socket lookup 0.099
  ms/op, all within the 1 ms/25 ms/0.25 ms budgets. Doctor found no running
  daemon or reachable Ollama; no model download occurred.
- 2026-09-05: completed Milestone 8's opt-in Zsh collection. `preexec` and
  `precmd` hooks classify only a fixed allowlist of Git, test, and build command
  forms; they retain no command text or arguments and asynchronously send only
  class, exit status, and duration. Test/build completion events preserve
  failures as bounded status evidence. The daemon compares actual Git
  index-tree and HEAD identities before emitting transition events, and emits
  `git.commit_completed` only when a successful classified commit also changed
  HEAD. Permission is checked before the event client snapshots Git, and hook
  client/socket failures are ignored. Transparent stdout/stderr capture was
  rejected because redirection or terminal interception would alter command
  semantics; no output permission or capture path exists. The real-Zsh
  integration now exercises the hooks, secret-bearing command syntax,
  transition detection, and unload cleanup. Focused race tests plus `go test
  ./...`, `make test`, and `make lint` passed. Benchmark results were parser
  0.311 us/op, exact snapshot 9.36 ms/op, deterministic generation 1.82
  ms/op, and warm socket lookup 0.117 ms/op, within the 1 ms/25 ms/0.25 ms
  budgets. Doctor found no running daemon or reachable Ollama; no model
  download occurred.
- 2026-09-05: completed Milestone 9's sole V1 Neovim adapter. Explicit Lua
  setup installs only file-open, file-save, and diagnostic-count-transition
  autocmds; it neither reads buffer content nor has a provider, renderer, or
  context store. It starts the existing consent-gated local emitter detached,
  so repository/worktree scope is freshly derived and checked by the established
  activity protocol. Paths, session IDs, and diagnostic severity counts are
  bounded; diagnostic prose is never passed to the emitter. Callback and
  emitter failures are contained, and `disable()` removes the dedicated
  augroup. The real-Neovim headless test covers emitted kinds/counts, diagnostic
  text exclusion, module loading, failure containment, and disable cleanup.
  The existing CLI/daemon consent tests cover the emitter's early default-deny
  gate before Git-state resolution. `go test -race ./internal/activity
  ./internal/ipc ./internal/daemon ./cmd/zsh-git-inlay`, `make test`, and
  `make lint` passed. Benchmark results were parser 0.302 us/op, exact snapshot
  8.72 ms/op, deterministic generation 1.53 ms/op, and warm socket lookup
  0.103 ms/op, within the 1 ms/25 ms/0.25 ms budgets. Doctor found no daemon or
  reachable Ollama; no model request or download occurred. `test-nvim` reports
  an explicit skip when Neovim is unavailable; this host ran it on Neovim 0.11.6.
- 2026-09-05: completed Milestone 10's local preference learning. A private,
  versioned repository profile holds only bounded aggregate style counts, with
  128 profiles at 16 KiB each and decay at 4096 observations; it stores no raw
  commit text, source, activity, remote, credentials, candidates, or provider
  state. The background daemon first applies repository policy and grounding,
  then uses explainable bounded local/historical style adjustments. The Zsh
  hook forwards no command text: it makes a best-effort, short-lived pre-commit
  snapshot request, and only an actual later HEAD transition lets the daemon
  reduce the final local commit subject/body before discarding it. Exact
  candidate matches are classified primary/alternate; same type/scope edited
  matches are explicitly documented as a weak inference. Aborts have no signal.
  Status, inspect, disable/enable, reset, repository-neutral export/import, and
  confirmation-gated local clone import are covered; clone matching strips
  credentials, query, and fragment data before comparing hashes. Focused race
  tests plus `make test` and `make lint` passed. Two benchmark runs recorded
  parser 0.300–0.462 us/op, exact snapshot 8.13–10.13 ms/op, deterministic
  generation 1.48–1.83 ms/op, and warm socket lookup 0.099–0.136 ms/op. All
  remain within 1 ms/25 ms/0.25 ms absolute budgets. The first parser and warm
  results exceeded the 25% relative threshold from M9, but the unchanged parser
  and second run returned to 0.300 us/op and 0.099 ms/op; [Inference] shared
  host scheduling is the strongest available explanation, not a demonstrated
  learning-path regression. Doctor found no daemon or reachable Ollama; no
  model request or download occurred.
- 2026-09-05: completed Milestone 11's explicit OpenAI cloud capability
  boundary without a live credential or request. A user-private `0600`
  provider-specific grant record accepts only a complete, confirmation-gated
  replacement class set; it contains no repository, source, endpoint, or
  credential data. Repository policy rejects cloud capability fields. The
  compiler maps only granted classes to its pre-redacted, relevance-filtered,
  staged-only sources; preview reports classes, source summaries, and byte
  bounds without content. `output_excerpts` remains a named future class with
  no current source, and an empty effective selection makes no request. The
  OpenAI Responses adapter reads `OPENAI_API_KEY` only after authorization at
  request time, uses non-streaming strict JSON with `store: false`, enforces
  prompt/response bounds, permits one retry only after a fresh exact staged
  check, and has no automatic fallback. Grant replacement/revocation cancels
  in-flight jobs, invalidates provider-derived records, and lookup reloads the
  grant before render; cache identity combines existing provider/model/prompt
  configuration identity with selected-class/redacted-prompt provenance. Mock
  tests cover contract shape, no request without grant/current-state proof,
  credential non-serialization, cancellation, supersession, per-provider
  isolation, redaction, preview, and revocation. `go test -race` for cloud,
  context, provider, daemon, and CLI packages; `make test`; and `make lint`
  passed. Three post-change benchmark runs recorded parser 0.204–0.261 us/op,
  exact snapshot 6.14–6.79 ms/op, deterministic generation 1.11–1.50 ms/op,
  and warm socket lookup 0.071–0.141 ms/op, all within the existing
  1 ms/25 ms/0.25 ms absolute budgets. [Inference] The spread is shared-host
  scheduling noise; cloud code remains off the ZLE/lookup path. `doctor --json`
  found no daemon or reachable Ollama. Live OpenAI compatibility, billing,
  availability, and model-quality validation remain unavailable by design; no
  key was used and no repository content was transmitted.
- 2026-09-05: completed Milestone 12's bounded hintable reranking and
  secondary body composition. The existing parser now selects only
  prefix-matching prepared candidates in their original order, without a
  daemon, context, generation, or questionnaire operation. `compose` is
  outside the ZLE path: it obtains a current prepared candidate, builds only a
  bounded `Staged paths:` status/path body tied to exact staged evidence,
  writes an owner-only `0600` file, and invokes a simple resolved Git-editor
  executable with literal arguments rather than a shell. Editor selection
  intentionally ignores repository-local `core.editor`; it accepts only
  user-controlled editor sources. It reads only the same private regular file,
  rejects a replacement, checks edited body wrapping and duplicate subject
  content, then re-snapshots the exact staged state.
  Mismatched state or policy leaves the edited file for the user and reports no
  verified result. Success reports that file and `committed: false`; neither
  staging nor `git commit` is invoked. A required-body policy suppresses all
  normal ghost text while retaining only candidates that failed solely for that
  missing body, so compose can complete them without admitting any other
  ungrounded candidate. Generated facts are grounded; preserved user prose is
  explicitly user-authored, not relabeled as generated evidence. Focused
  race-enabled tests covered matching-prefix cycling, malformed/duplicate body
  rejection, private and symlink file handling, repository-local editor
  rejection, safe editor parsing, no-commit, staged-state change, and
  required-body behavior. `go test -race
  ./internal/command ./internal/grounding ./internal/daemon
  ./internal/compose ./cmd/zsh-git-inlay`, `make test`, and `make lint` passed;
  the full test target also passed real-Zsh integration, daemon recovery, and
  evaluation integration. The final benchmark recorded parser 0.210 µs/op,
  exact snapshot 6.08 ms/op, deterministic generation 1.10 ms/op, and warm
  socket lookup 0.067 ms/op, within the 1 ms/25 ms/0.25 ms budgets. `doctor
  --json` found no running daemon or reachable Ollama; no model, cloud, or paid
  request occurred.
- 2026-09-05: completed Milestone 13's local release-engineering preparation.
  `version --json` exposes linker-embedded version and commit metadata; normal
  builds use `-trimpath`, disabled VCS metadata, and a fixed build ID. A
  source-only installer atomically replaces only the named binary/plugin files
  and refuses root prefixes. Its paired uninstaller stops the daemon when
  possible, removes only those installed files by default, and requires an
  explicit purge flag before deleting exact product-named standard XDG
  directories. `release-snapshot` refuses an existing output directory,
  cross-compiles Linux/Darwin amd64/arm64 archives, normalizes archive inputs,
  emits SHA-256 checksums and a deterministic SPDX 2.3 SBOM, and neither tags,
  publishes, nor downloads anything. The install lifecycle test covers clean
  install, rerun upgrade, root-prefix refusal, clean uninstall, explicit
  purge boundaries, release-output refusal, checksums, all four archives,
  embedded packaged metadata, and byte-identical dual snapshots. It now runs
  under `make test`; lint checks all added shell files. `go test -race
  ./cmd/zsh-git-inlay`, `make test`, `make lint`, `make test-install`, and a
  direct `make release-snapshot` passed on Fedora. Two post-change benchmark
  runs recorded parser 0.307–0.561 µs/op, exact snapshot 7.66–14.53 ms/op,
  deterministic generation 1.46–2.39 ms/op, and warm lookup 0.094–0.193
  ms/op: all meet the 1 ms/25 ms/0.25 ms absolute budgets but exceed the M12
  relative comparison threshold. [Inference] Broad variation across unchanged
  benchmark packages on this shared host is the strongest available
  explanation; release code is outside the interactive path. `doctor --json`
  found no daemon or reachable Ollama, and no model/cloud request occurred.
  The GitHub Actions Linux/macOS workflow is checked in but has not run from
  this checkout, and macOS runtime validation remains external. The repository
  has no license file, so artifact redistribution and publication remain
  explicitly blocked pending maintainer choice rather than being inferred.
