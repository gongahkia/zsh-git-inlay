# Next implementation work

Scope: implement the code changes from recommendations 1–6; commit each
completed task/subtask separately. Do not modify README.md. Publishing a release,
choosing a license, collecting human ratings, and qualifying a live model on
specific hardware remain maintainer activities; provide executable tooling and
accurate evidence for them.

## Tasks

- [ ] 1. Richer staged-only deterministic suggestions, retaining the frozen
  metadata generator as a comparison baseline. Cover dependencies, renames,
  deletions, added declarations, and meaningful alternatives.
- [ ] 2. Configurable local generation budgets, cold/warm behavior, timing and
  stop-reason diagnostics, and reproducible model qualification tooling.
- [ ] 3. Setup/check/demo workflow, installation-aware doctor, verified binary
  installation/package preparation, and release automation without publishing.
- [ ] 4. Repository-specific status/explain with actionable quiet-state and
  provider-failure explanations.
- [ ] 5. Cheap shell prefilter, configurable emacs/vi cycling, full-path
  performance and compatibility tests, recurring race/fuzz CI.
- [ ] 6. Expanded held-out evaluation and private blinded human comparison
  tooling for factual adequacy, specificity, editing effort, and usable-message
  time. Distinguish explicit ratings from inferred learning signals.

## Validation

Run targeted checks before each implementation commit and the full available
Go/Zsh/install/evaluation/lint suites at integration. Record platform, unavailable
dependencies, and live/human evidence boundaries. Verify README.md against the
starting commit (61caedd) and leave a clean tree.

## Evidence

- Starting tree clean at 61caedd; host macOS amd64, Go 1.27.0, system Zsh.
