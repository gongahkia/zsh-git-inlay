# Prototype behavior and verification

## What the prototype proves

The deterministic provider has three distinct ordered candidates per exact staged state. Preparation begins from the prompt boundary after a Git command returns, before `git commit -m ` is typed. A normal commit command only performs an exact index identity plus a bounded local cache lookup. No candidate is generated on that path.

The automated suite maps to the acceptance scenarios as follows:

| Scenario | Evidence |
| --- | --- |
| A: predictive appearance | `tests/integration.zsh` starts observation, waits for `candidates`, then invokes the registered real-Zsh strategy for `git commit -m ` and requires a prepared suggestion. |
| B: cycling and acceptance | The same test changes candidate selection, invokes the standard autosuggestions accept function, and verifies the buffer has the correctly quoted candidate without executing Git. |
| C: stale rejection | The test stages a second file after preparing state A and requires the strategy to yield until state B is prepared. `internal/daemon` separately proves a superseded state cannot publish. |
| D: passive coexistence | The integration test preserves an existing history strategy and verifies unrelated commands yield. |
| E: isolation | `internal/gitstate` covers separate repositories and linked worktrees; `internal/daemon` verifies scope checks reject a cross-scope cache lookup. |

Run all checks with `make test && make lint`. `make bench` records parser/suggestion, exact fingerprint, and deterministic generation benchmarks. The benchmark is a regression signal for accidental work in the interactive path; deterministic provider timing is not a prediction of future LLM latency.

## Representative measurements

On Fedora Linux 43, an Intel Core i7-1355U, Go `1.26.7`, Git `2.55.0`, and Zsh `5.9`, the final verification run measured:

| Operation | Result |
| --- | --- |
| Parser/suggestion composition | 0.65 µs/op (`make bench`) |
| Exact fingerprint | 23.13 ms/op (`make bench`) |
| Deterministic metadata generation | 4.13 ms/op (`make bench`) |
| Warm Unix-socket cache lookup round trip | 0.208 ms/op (`make bench`) |
| Full helper `suggest` path (new process, fingerprint, warm lookup) | 11 ms mean over 100 calls |
| Helper `fingerprint` path | 12 ms mean over 100 calls |
| Helper `status` local IPC path | 3 ms mean over 100 calls |
| Lazy daemon start through candidates-ready | 56 ms in the temporary one-file fixture |

The strategy performs its helper work in `zsh-autosuggestions` asynchronous mode, which the dependency enables by default on Zsh 5.0.8 and later. These are local prototype measurements, not an LLM-latency forecast; repository size, filesystem, and process-launch cost materially affect them.

## Renderer limitation and manual PTY demonstration

The automated Zsh test verifies the actual `zsh-autosuggestions` strategy and acceptance contract but does not inspect terminal pixels. Terminal rendering is owned by the dependency and varies with terminal themes and ZLE setup. This is an intentional, documented test boundary rather than a claim that a non-PTY test proves colors.

For a deterministic local demonstration after installation:

```zsh
tmp=$(mktemp -d)
git -C "$tmp" init -b main
print -r -- prototype > "$tmp/cache.txt"
git -C "$tmp" add cache.txt
cd "$tmp"
zsh-git-inlay observe --cwd "$tmp"
# wait until: zsh-git-inlay candidates --cwd "$tmp"
# then type the commit-message prefix: git commit -m
# the configured autosuggestions highlight displays the primary prepared candidate.
```

Press the normal autosuggestion forward-character/end-of-line binding to accept, `^Xg` to cycle, or keep typing. Do not run the displayed commit in this demonstration if an uncommitted test repository must remain untouched.

## Known limitations

- The provider is factual only at the status/path level. It neither reads staged source content nor claims semantic accuracy.
- Git index identity is exact but requires short Git plumbing calls and a private copy of the index (capped at 64 MiB); default autosuggestions async mode keeps those calls out of synchronous ZLE handling. Explicitly disabling autosuggestions async mode can make the strategy visibly slower on very large indexes.
- The strategy starts a helper process for each non-optimized autosuggestion fetch. It has a short socket deadline and no diff/generation work, but it is not a zero-cost in-process API.
- Explicit configured shell aliases and Git configuration aliases are not implemented. Canonical `git commit` forms are the supported grammar; executable Git aliases are never run.
- The fallback runtime cache is private but less ideal than a correctly configured `XDG_RUNTIME_DIR`; its parent-directory trust follows local XDG permissions.
- No real terminal-pixel assertion is in CI; the strategy/widget contract and manual PTY procedure cover that boundary.
- Git cannot provide a non-blocking atomic read lock across final fingerprint validation and cache rename. A narrow post-check race can retain an obsolete cache record, but exact lookup rejects it before rendering; see the architecture document for the deliberate tradeoff.
