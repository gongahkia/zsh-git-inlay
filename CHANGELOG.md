# Changelog

This project follows [Semantic Versioning](https://semver.org/). Release notes
are written before a tag is created; generated snapshots are not releases.

## Unreleased

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
