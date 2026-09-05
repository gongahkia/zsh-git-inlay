# RC1 execution plan

## Scope and decision standard

RC1 validates the existing V1 architecture; it does not add another frontend,
automatic Git actions, telemetry, or a broad product capability. Evidence is
classified as implemented and locally verified, mock/contract verified,
prepared but externally unverified, or deliberately rejected. The final
classification must be one of the three release labels in the RC1 objective.

## Audited starting state — 2026-09-05

The audited starting commit is `8f6548e` on `main`. The working tree is clean.
Contrary to the supplied V1 handoff, local `origin/main` also resolves to
`8f6548e`: its reflog records `update by push` at 21:22 +0800 on 2026-09-05.
This RC1 pass did not perform that push and will not mutate a remote. The
history remains linear from `8dc6d2a` through the V1 commits; it will not be
rewritten.

Baseline local evidence passed on this Fedora 43 host: `make test`, `go test
-race ./...`, `make lint`, `make bench`, `git diff --check`, `doctor --json`,
and a fresh four-target release snapshot. Benchmark values were parser 0.270
us/op, exact snapshot 7.61 ms/op, deterministic generation 1.38 ms/op, and
warm socket lookup 0.091 ms/op. They remain within the 1 ms/25 ms/0.25 ms
budgets; shared-host variation remains an explicit limitation.

## Evidence gaps and gated work

| Gate | Local work | External boundary or current finding |
| --- | --- | --- |
| invariants/cache | audit code, tests, cache retention, stale publication and documentation | no external dependency expected |
| live Ollama | inspect installed runtime/models; exercise only an already available local runtime | `ollama` is absent. Installing it and downloading bounded model weights requires maintainer approval after model/size/license review. |
| evaluation/grounding | separate held-out and adversarial synthetic evidence; report deterministic results | live-model quality remains unavailable without an approved local model. |
| Zsh dogfooding | real Zsh/autosuggestions automated contract and PTY evidence where the host supports it; add maintainer visual checklist | terminal-pixel appearance and subjective usefulness require a human terminal review. |
| fuzzing | native bounded Go fuzz campaigns and invariant/property tests | no external dependency expected |
| static/supply chain | `go vet`, project lint, ShellCheck, workflow/release/script/dependency review | installed Staticcheck is built with Go 1.25 and rejects this Go 1.26 module; `govulncheck` and `actionlint` are absent and will not be installed without approval. Remote CI remains unexecuted. |
| install/release | isolated install, reinstall, purge, archive, checksum, SBOM, path and permission rehearsal | macOS artifacts can be cross-compiled but not runtime-tested on this Fedora host. |
| cloud | mock/adversarial grant, revocation and payload checks only | no cloud credential, consent, or request is authorized. |
| licensing | inspect dependencies and distribution boundaries; provide maintainer decision support | a maintainer must select a project license before redistribution. |

## Planned local changes

1. Fix demonstrated release-script lint findings without changing install or
   uninstall behavior.
2. Add bounded fuzz/property coverage for untrusted parser, IPC, provider,
   policy, event, learning, and quoting inputs, then run bounded campaigns.
3. Strengthen synthetic adversarial and held-out evaluation evidence without
   committing private repository history or generated reports.
4. Add an automated Zsh dogfood/PTY boundary where reliable and a concise
   maintainer visual checklist for what automation cannot observe.
5. Add release, license, reliability, evaluation, privacy, and README claim
   audits that name the precise unverified conditions.

## Gate 1 invariant audit

| Invariant | Code and test evidence |
| --- | --- |
| No global `git()` override or `eval` | `zsh-git-inlay.plugin.zsh` is a strategy provider; `make lint` rejects both patterns and `tests/integration.zsh` asserts no `git` function. |
| ZLE does not generate, scan, access a network, or wait for daemon startup | The strategy calls only `suggest`; `precmd` backgrounds `observe` with `&!`. `suggest` does the bounded current-fingerprint/socket lookup, while `daemon.generate` owns context compilation/provider calls. Integration/dogfood tests require a prepared candidate and no stale render. |
| Candidates are exact-fingerprint-bound and stale ones cannot render | `gitstate.Snapshot` includes repository/worktree, HEAD, index, configuration, and context identities; `TestSnapshotTracksExactStagedState`, `TestSupersededStateCannotPublish`, and Zsh integration cover index change/rejection. |
| Repository and linked-worktree isolation holds | Records retain repository/worktree IDs and `lookup` checks both; `TestSnapshotIsolatesRepositoriesAndLinkedWorktrees` and `TestObservePublishesAtomicScopedCandidates` cover collisions and cross-scope lookup. |
| Activity is default-deny and output needs separate permission | `activity.Permissions` defaults false; no output-capture class is implemented. `TestActivityRequiresConsentAndRevocationClearsMemory`, `TestPermissionsCommandsPersistOnlyTheActivityGrant`, and activity hook tests cover the boundary. |
| Repository policy cannot grant capability | The policy parser accepts only declarative commit shape fields; `TestRepositoryConfigRejectsCapabilities` rejects provider/activity/cloud keys. |
| Cloud is never a silent fallback | `openai` requires an explicit grant and `fallback = "none"`; `TestCloudGenerationRequiresGrantAndRevocationRejectsCachedCandidate` and OpenAI contract tests cover grant, cancellation, and no request before authorization. |
| Compose never stages or commits | Compose writes a private message file and rechecks state; `TestComposeEditsGroundedBodyWithoutCommitting` and `TestComposeRejectsStagedStateChangedInEditor` cover it. |
| Malformed model output cannot reach the command buffer | Provider JSON/schema/candidate validation precedes grounding/ranking; Ollama malformed/trailing tests, IPC tests, parser tests, and provider fuzzing cover this boundary. |
| Cache, socket, and sensitive files are owner-private | Runtime and cache code require private paths; `TestObservePublishesAtomicScopedCandidates`, `TestPermissionRecordIsPrivateAndStrict`, runtime tests, and installation rehearsal check permissions and path handling. |
| Obsolete cache retention is bounded | Final-check mismatch calls `discardRecord`; residual post-check records are protected by exact lookup and deterministic startup/post-write age/count/byte collection. Cache-GC tests exercise every removal mode. |

## Rejected approaches

- Installing Ollama or downloading a model before explicit maintainer approval.
- Sending a cloud request, inspecting credential contents, or using a paid API.
- Treating cross-compilation, mocked providers, non-PTY Zsh tests, or a remote
  workflow definition as equivalent to live model, visual terminal, macOS, or
  GitHub Actions validation.
- Choosing a project license on the maintainer's behalf.

## Progress

- 2026-09-05: completed initial state, history, tool, documentation, provider,
  installer, workflow, and release-tool audit. Confirmed the post-V1 remote
  ref update, clean local tree, absent Ollama, and available `staticcheck` /
  `shellcheck` executables. Baseline tests and release snapshot passed.
- 2026-09-05: `staticcheck ./...` could not analyze the module because the
  installed executable reports Go 1.25 while the module requires Go 1.26.
  ShellCheck found four command-substitution style warnings and one literal
  output warning in release scripts; those are locally actionable.
- 2026-09-05: fixed the ShellCheck findings, then ran ShellCheck cleanly. A
  fresh Go 1.26 Staticcheck invocation found and verified fixes for three
  ignored private-file chmod/write errors and three error-message style
  findings. Fresh `govulncheck ./...` reported no vulnerabilities and
  `actionlint .github/workflows/ci.yml` passed. `go mod verify` passed; the
  module graph has no third-party Go modules. The workflow now pins the
  inspected `actions/checkout`, `actions/setup-go`, and
  `actions/upload-artifact` v7 commits while preserving `contents: read`.
- 2026-09-05: added seven native Go fuzz targets. Bounded 3-second
  single-worker campaigns completed for command parsing/suggestion (157k
  executions), IPC framing (118k), structured provider output (44k), config
  values (136k), activity events (137k), learning import/remote normalization
  (78k), and context redaction/bounds (9.2k). No crash, hang, allocation-limit,
  or asserted invariant failure was found. An earlier all-target run exceeded
  the terminal command time budget after three completed targets; the final
  `make fuzz FUZZ_TIME=3s` completed all seven targets in 23.8 seconds.
- 2026-09-05: split the public synthetic corpus into 8 development and 7
  held-out cases, including partial staging, misleading comments,
  shell-metacharacter paths, issue-like text, and staged prompt injection.
  Deterministic held-out evaluation produced 21 candidates with every
  applicable automatic check passing, no error/timeout, 1.784 ms cold and
  1.726 ms warm average latency, and 326,128 bytes approximate allocation per
  case. A 20-commit local-history replay produced 60 candidates without error
  or timeout; its 80% component metric is recorded as a diagnostic rather than
  a quality claim. No prompt iteration or live-model comparison occurred.
- 2026-09-05: expanded grounding adversarial tests to reject unsupported test
  outcomes, unstaged-work claims, unsupported fix/race/outage/performance/
  security claims, unknown issue identifiers, and unsupported scopes. The
  product documentation continues to distinguish evidence checks and
  heuristics from semantic proof.
- 2026-09-05: `make test-dogfood` passed repeatedly using an installed plugin,
  real isolated Zsh sessions, and `zsh/zpty` terminal output. It exercises
  staged preparation, ghost-text availability, acceptance/cycling widgets,
  partial intent, quote completion, invalidation, multiple sessions, restart,
  and uninstall. It cannot prove terminal pixels, user keymaps, or subjective
  usefulness; `RELEASE-CHECKLIST.md` contains the outstanding human checks.
- 2026-09-05: source and archive install rehearsals passed in temporary XDG
  directories with a prefix containing spaces, reinstall, default retention,
  explicit purge, a permission-denied prefix when applicable, archive member
  validation, checksums, SPDX SBOM, and reproducible four-platform snapshots.
  The Linux archive installer was exercised; macOS binaries were only
  cross-compiled.
- 2026-09-05: inspected Fedora's available Ollama package (0.9.4, 61 MiB
  download / 772.8 MiB installed), host disk (715 GiB free), and empty Ollama
  model manifests. The executable is absent. Official Ollama manifests show
  398 MB Qwen2.5-Coder 0.5B, 986 MB Qwen2.5-Coder 1.5B, and 398 MB Qwen2.5
  0.5B comparison candidates, each marked Apache-2.0. No runtime or model was
  installed or downloaded; the 1.782 GB bounded comparison needs maintainer
  approval. No OpenAI credential was inspected and no cloud request was made.
- 2026-09-05: added `LICENSE-REVIEW.md` and `RELEASE-CHECKLIST.md`; audited
  README, installation, privacy, reliability, provider, activity, threat, and
  evaluation claims. The release archive excludes model weights, Ollama,
  autosuggestions, activity/cache data, local evaluation data, and the
  optional Neovim adapter. The repository still has no project license.

## Release recommendation

Pending the remaining local gates, the current expected classification is
**TECHNICALLY READY, EXTERNALLY BLOCKED**, not RELEASE-READY: a maintainer
license decision is mandatory for redistribution, and live-model, remote-CI,
macOS-runtime, and visual-terminal evidence are absent. This is a provisional
assessment, not the final RC1 decision.
