# Next implementation work

Scope: implement the code changes from recommendations 1–6; commit each
completed task/subtask separately. Do not modify README.md. Publishing a release,
choosing a license, collecting human ratings, and qualifying a live model on
specific hardware remain maintainer activities; provide executable tooling and
accurate evidence for them.

## Tasks

- [x] 1. Richer staged-only deterministic suggestions, retaining the frozen
  metadata generator as a comparison baseline. Cover dependencies, renames,
  deletions, added declarations, and meaningful alternatives.
- [x] 2. Configurable local generation budgets, cold/warm behavior, timing and
  stop-reason diagnostics, and reproducible model qualification tooling.
- [x] 3. Setup/check/demo workflow, installation-aware doctor, verified binary
  installation/package preparation, and release automation without publishing.
- [x] 4. Repository-specific status/explain with actionable quiet-state and
  provider-failure explanations.
- [x] 5. Cheap shell prefilter, configurable emacs/vi cycling, full-path
  performance and compatibility tests, recurring race/fuzz CI.
- [x] 6. Expanded held-out evaluation and private blinded human comparison
  tooling for factual adequacy, specificity, editing effort, and usable-message
  time. Distinguish explicit ratings from inferred learning signals.

## Validation

Run targeted checks before each implementation commit and the full available
Go/Zsh/install/evaluation/lint suites at integration. Record platform, unavailable
dependencies, and live/human evidence boundaries. Verify README.md against the
starting commit (61caedd) and leave a clean tree.

## Evidence

- Starting tree clean at 61caedd; host macOS amd64, Go 1.27.0, system Zsh.

## Completed code audit

| Recommendation | Implementation and direct evidence |
| --- | --- |
| 1 | `internal/provider/facts.go`; staged/unstaged dependency isolation, exact identifier/file spelling, rename/delete and primary-candidate tests; frozen generator and v2 corpus unchanged. |
| 2 | Validated `[ollama]` budgets, cold/warm tests and timing/stop metadata; full daemon/socket test rejects truncated output then publishes grounded recovery; `scripts/qualify-model.py` gates development before three held-out runs. |
| 3 | `setup --check --demo`, installation-aware `doctor --check`, actual installed disposable-demo test; reviewed-digest binary installer, license-gated Homebrew preparation and read-only artifact workflow; reproducible native archive install/upgrade/uninstall tests. |
| 4 | Repository `status --cwd --buffer`, quiet-state `explain`, provider/policy failure reasons and actions; tests verify unprepared/failure/policy/prefix states without generation. |
| 5 | Executable-marker test proves unrelated typing never launches the helper; idempotent sourcing and emacs/vi binding restoration; small/10,000-entry full-helper mean/p95 benchmarks; scheduled race/fuzz CI and macOS test portability. |
| 6 | Versioned 27-case corpus with 19 held-out cases; explicit v2 replay; private randomized review packets, separate hash-bound keys, validated explicit scores and completion counts; CLI review/history identity tests. |

## Final validation evidence

- macOS amd64: Go 1.27.0, Zsh 5.9, Neovim 0.12.4, Apple Git 2.39.5
  via `/Library/Developer/CommandLineTools/usr/bin`. The system Git launcher
  added overhead; the actual test Git path is recorded explicitly.
- zsh-autosuggestions v0.7.1, upstream commit
  `e52ee8ca55bcc56a17c828767a3f98f22a68d4eb`, used from a temporary checkout.
- `make test lint` passed, including Go, installed setup/demo, real Zsh,
  Neovim, evaluation, release installation, installed PTY dogfooding and soak.
- Full `go test -race -p 1 ./...` passed; targeted race checks passed for final
  factual generation, history/CLI review, and Ollama recovery tests.
- All seven native fuzz targets passed with `FUZZ_TIME=1s`.
- Python packaging/qualification tests passed (7 tests), including bad-checksum
  rejection before installation, deterministic archive metadata, selected-license
  preservation, development-gate stopping, and required held-out run counts.
- After installer follow-ups, `make test-install test-setup lint` passed again,
  including byte-identical four-target snapshots and native macOS execution.
- ShellCheck, actionlint for both workflows, `git diff --check`, and comparison
  against the starting README/frozen generator/frozen corpus passed.
- Full-helper 10-sample results are recorded in `RELIABILITY.md`.

## Manual/external work

No license was chosen, release published, remote CI run, cloud request made,
model downloaded or human rating fabricated. Ollama is unavailable on this
host, so its changed transport is validated with loopback contract and daemon
recovery tests; real hardware qualification uses the new explicit script.
Human factual-usefulness/preferences and terminal review, license selection,
Homebrew install against actual published artifacts, remote CI, and independent
review remain maintainer activities. The public synthetic partition is regression
and future model-selection infrastructure, not a claim of human usefulness.

Each implementation/fix/test subtask is committed separately. Use
`git log --oneline 61caedd..HEAD` to inspect the rollback history.
