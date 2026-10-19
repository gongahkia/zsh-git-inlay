# Changelog

This project follows [Semantic Versioning](https://semver.org/). Release notes
are written before a tag is created; generated snapshots are not releases.

## Unreleased

- Added staged-facts suggestions and an unchanged metadata comparison provider;
  configurable local inference budgets/timings and model qualification gates;
  repository-specific quiet-state explanations; installed setup/check/demo;
  checksum-pinned binary and Homebrew preparation; emacs/vi cycling, a shell
  prefilter, full-helper p95 benchmarks and scheduled race/fuzz CI; expanded
  versioned evaluation and private blinded human review.
- Verified macOS amd64 runtime and installation, fixed platform test/socket
  assumptions and deterministic archive metadata, and retained selected license
  notices through installation/uninstallation. No license or release selected.

- Ran the approved loopback-only Ollama comparison. Qwen2.5-Coder 0.5B,
  Qwen2.5-Coder 1.5B, and Qwen2.5 0.5B failed the frozen development gate;
  deterministic remains the default.
- RC1 adds held-out/adversarial evaluation partitions, bounded native fuzz
  targets, installed-plugin Zsh dogfooding, release-archive installation
  rehearsal, action SHA pinning, and a release/license evidence audit.
- Fixed ignored private-file permission/write errors found by Staticcheck in
  compose, evaluation-report, and managed-model persistence paths.
- Hardened exact staged-state cache retention and daemon recovery.
- Added deterministic evaluation, local Ollama and explicit cloud-provider
  boundaries, bounded context, evidence-backed grounding, repository policy,
  opt-in activity, the Neovim event adapter, and local preference learning.
- Added prefix-conditioned prepared-candidate reranking and a non-committing,
  grounded commit-body composition workflow.
- Added source installation, upgrade, uninstall, reproducible release snapshot,
  checksums, SPDX SBOM, and CI preparation.

## Release policy

The first published version and tag require maintainer approval, a selected
license, clean CI evidence, reviewed changelog entries, and verified checksums.
Normal releases use `vMAJOR.MINOR.PATCH`: breaking behavior increments MAJOR,
backward-compatible features increment MINOR, and fixes increment PATCH.
