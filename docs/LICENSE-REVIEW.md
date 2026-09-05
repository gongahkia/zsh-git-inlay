# License review

This is engineering decision support, not legal advice. It records the RC1
source and distribution inventory so the maintainer can make an informed choice
and obtain legal advice where needed.

## Current finding

The repository has no `LICENSE` file and no maintainer-selected project
license. `go list -m all` at RC1 reports only
`github.com/gongahkia/zsh-git-inlay`; there are no required third-party Go
modules. The snapshot SBOM accurately declares the application package but
uses `NOASSERTION` for its license. Source snapshots contain only the Go
executable, Zsh plugin, README, and changelog. They contain no model weights,
Ollama binary, `zsh-autosuggestions` source, cache, activity, configuration,
evaluation reports, or Neovim adapter.

Accordingly, source use and local validation are possible, but artifact
redistribution is not legally ready. RC1 is blocked on an explicit maintainer
choice; this file does not make that choice.

## Dependency and distribution inventory

| Item | RC1 evidence | License/distribution implication |
| --- | --- | --- |
| This project | No project license is present. All Go imports resolve to the standard library or this module. | A license and rights/provenance decision is required before redistribution. |
| `zsh-autosuggestions` | Required runtime dependency; Fedora 43 package `0.7.1-3.fc43` identifies MIT. It is sourced from the user's system and is not copied into source snapshots. | A project license compatible with MIT is appropriate. If a future package bundles it, retain its copyright and MIT notice. |
| Ollama | The project talks only to an explicitly selected loopback runtime and snapshots do not ship it. Upstream's source license is MIT; the inspected Fedora package metadata is `Apache-2.0 AND MIT`. | Interaction with a separately installed runtime is not the same as redistribution. Bundling a runtime later requires a version-specific license, notices, dependency review, and provenance check. |
| `llama.cpp` / managed runtime | No `llama.cpp` artifact or authenticated managed manifest is included or downloadable by this project. Upstream currently publishes an MIT license. | The managed-runtime design is not redistribution authority. Any future binary bundle needs an exact release/digest, all embedded dependency notices, and a legal review. |
| Candidate Qwen models | No weights are downloaded or bundled. The reviewed Ollama manifests for `qwen2.5-coder:0.5b`, `qwen2.5-coder:1.5b`, and `qwen2.5:0.5b` display Apache-2.0. | A model card/manifest is not a blanket license conclusion for every tag or quantization. Pin the exact digest, retain applicable terms/notices, and review redistribution rights before shipping weights. |
| Neovim activity adapter | Repository-authored Lua under `lua/`; it requires Neovim but declares no third-party Lua dependency. It is deliberately not in the current Zsh release archive. | It is an optional event producer, not a second suggestion frontend. Decide whether a future distribution should include it and document its install path; do not imply archive installation provides it. |
| Test fixtures and adapted code | No copied-code attribution or third-party fixture license appears in the tree; fixtures are synthetic or generated during tests. | This is an incomplete provenance signal, not proof. The maintainer must attest that contributed code and fixtures can be relicensed. |
| GitHub Actions | Actions are build infrastructure, not release-archive contents. RC1 pins official `checkout`, `setup-go`, and `upload-artifact` commits. | Keep action review separate from application licensing; pinning does not assign a project license. |

References reviewed in RC1: [Ollama license](https://github.com/ollama/ollama/blob/main/LICENSE), [llama.cpp license](https://github.com/ggml-org/llama.cpp/blob/master/LICENSE), and the relevant [Qwen2.5-Coder Ollama manifest](https://ollama.com/library/qwen2.5-coder:0.5b).

## Practical project-license options

| Option | Fit | Tradeoff |
| --- | --- | --- |
| Apache-2.0 | Recommended engineering default. It is compatible with the observed MIT dependency and includes an express patent grant. | Requires retaining the license text and any applicable `NOTICE`; contributors and maintainers should understand the patent clause. |
| MIT | Simple and compatible with the observed dependency. | No express patent grant and fewer attribution conventions for a project that may later integrate runtimes/models. |
| MPL-2.0 | File-level copyleft may suit a project wanting reciprocity without whole-work copyleft. | Adds compliance complexity and needs a deliberate policy for mixed plugin/runtime distribution. |
| GPL-family | Can be appropriate if strong copyleft is the maintainer's intent. | Raises broader compatibility and distribution questions for optional components; do not select it casually. |

Recommendation: the maintainer should consider **Apache-2.0** for the project
source and its own release archives, subject to contributor-rights and legal
review. This is a practical compatibility recommendation, not a legal
conclusion and not a license selection made by RC1.

## Exact unresolved decision and follow-through

The maintainer must select a project license and confirm that project
contributors, fixtures, documentation, and any adapted code can be distributed
under it. If Apache-2.0 is selected, the minimum follow-through is:

1. Add the canonical Apache-2.0 text as `LICENSE` at the repository root.
2. Add a `NOTICE` file only if required by included notices or the maintainer's
   attribution policy; preserve third-party notices whenever code is bundled.
3. Update `scripts/sbom.sh` so `licenseDeclared` and `licenseConcluded` name
   the selected SPDX identifier, then regenerate and inspect snapshots.
4. Update README, installation, release checklist, and changelog wording from
   “no license selected” to the actual license and archive notice locations.
5. Decide whether repository source files receive SPDX headers, apply the
   policy consistently, and verify existing copyright/provenance records.
6. If a runtime, adapter, or model is ever bundled, repeat this review against
   the exact version, digest, notices, model terms, and redistribution path.

Until those actions are complete, do not publish release archives, model
weights, package-manager artifacts, or a release tag representing a public
redistribution.
