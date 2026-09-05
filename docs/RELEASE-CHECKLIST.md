# RC1 release checklist

## Current classification

**TECHNICALLY READY, EXTERNALLY BLOCKED** is the RC1 recommendation once the
local final verification in `EXECPLAN-RC1.md` is complete. It is not
RELEASE-READY because the project has no selected license, no live local-model
result, no remote CI execution, no macOS runtime execution, and no maintainer
visual/usability or independent review. No release is created by this checklist.

## Local RC1 evidence

| Item | Status |
| --- | --- |
| Exact staged fingerprint, repository/worktree isolation, stale rejection, private socket/cache, default-deny activity/cloud controls, and non-committing compose | Locally tested and code-audited |
| Deterministic held-out evaluation | 7 cases / 21 candidates; all applicable automatic checks pass; no timeout/error |
| Ollama and OpenAI | Mock/contract tested only; no runtime/model download or cloud request |
| Fuzzing | Seven bounded native targets pass; no findings |
| Static/supply-chain checks | `go vet`, fresh Staticcheck, `govulncheck`, ShellCheck, YAML/actionlint, `go mod verify`, and action SHA review pass locally |
| Zsh dogfooding | Installed plugin, real Zsh sessions, and pseudo-terminal ghost-text output pass; no human pixel review |
| Install/release rehearsal | Source/archive install, spaces, reinstall, data retention/purge, permission-denied path, checksum/SBOM/archive checks, and four reproducible cross-compiled archives pass on Fedora |
| Remote CI and macOS runtime | Not run |

## Maintainer visual checklist

Run these in a normal interactive Zsh terminal after sourcing
`zsh-autosuggestions` and this plugin. Mark a result only after visually
observing it; automated pseudo-terminal output does not substitute for this.

1. Stage a small change, wait for background preparation, and type `git commit -m `.
   Confirm a dim passive suggestion appears without a typing pause.
2. Use the normal forward-character and end-of-line bindings to accept a
   suggestion. Confirm quote completion and that no command executes until
   Enter is deliberately pressed.
3. Cycle prepared candidates with `^Xg`, then type a partial subject. Confirm
   matching candidates remain passive and unrelated commands receive none.
4. Change the index after a candidate is visible. Confirm it disappears until
   a new exact staged state is prepared.
5. Restart or stop the daemon, use two shells in one repository, then two
   repositories and a linked worktree. Confirm no stale or cross-scope text.
6. Confirm normal history autosuggestions remain available after the
   `git-inlay` strategy, unload the plugin, and confirm the original behavior
   returns.
7. Check theme contrast, terminal width/wrapping, configured keymaps, and the
   perceived usefulness/conservatism of suggestions. Record terminal, Zsh, and
   `zsh-autosuggestions` versions with any issue.

## External gates

### License decision

Select a license and complete the exact follow-through in
[LICENSE-REVIEW.md](LICENSE-REVIEW.md). This is mandatory before public
redistribution.

### Approved live local-model rehearsal

Do this only after the maintainer explicitly approves installing Fedora's
Ollama package and downloading the bounded comparison set. RC1 inspected
Fedora metadata: Ollama 0.9.4 is a 61 MiB download / 772.8 MiB installed;
three reviewed models total about 1.782 GB (398 MB + 986 MB + 398 MB). No paid
service, cloud provider, source upload, telemetry upload, or remote install
script is needed.

```zsh
sudo dnf install ollama
ollama serve
ollama pull qwen2.5-coder:0.5b
ollama pull qwen2.5-coder:1.5b
ollama pull qwen2.5:0.5b
zsh-git-inlay doctor --json
zsh-git-inlay evaluate --fixtures --provider ollama --model qwen2.5-coder:0.5b --partition development --json
zsh-git-inlay evaluate --fixtures --provider ollama --model qwen2.5-coder:0.5b --partition held-out --json
```

Repeat the two evaluation commands for each candidate model and record cold
load, warm generation, time-to-first-token when the runtime exposes it,
process-resident memory, cancellation, timeout, malformed output, daemon
restart, staged-state supersession, model removal, and deterministic fallback.
Do not declare a default local model without this comparison and manual review
of the held-out candidates.

### Remote CI and macOS

Do not push merely to obtain this evidence. When a maintainer chooses to run
the existing workflow on GitHub, review the actual Linux/macOS matrix results
and snapshot artifact from that run. On a real macOS host, run:

```zsh
make test
go test -race ./...
make lint
make test-install
make test-dogfood
make release-snapshot VERSION=verification-rc1 DIST=.build/release-macos-rc1
```

Cross-compilation alone is not macOS runtime validation.

### Independent review

Before a public release, obtain separate security and usability review. The
local threat-model, fuzz, static-analysis, and terminal checks reduce risk but
are not an independent assessment.

## Final local preflight

```zsh
make test
go test -race ./...
make lint
make bench
make test-integration
make test-eval
make test-soak
make test-install
make fuzz FUZZ_TIME=3s
make release-snapshot VERSION=verification-rc1 DIST=.build/release-rc1
git diff --check
go run ./cmd/zsh-git-inlay doctor --json
```

Inspect the snapshot's archive members, SHA-256 checksums, SPDX SBOM, version
metadata, and strings for credentials, local paths, private evaluation data,
cached activity, and model weights. A clean preflight does not clear the
external gates above.
