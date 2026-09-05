# Reliability and evaluation evidence

`make test` runs Go package tests, the real-Zsh interaction and daemon-restart
tests, synthetic evaluation integration, Neovim headless validation when
available, installation/release lifecycle checks, and a bounded multi-shell
soak. The targets below are useful when isolating a failure:

```zsh
make test-integration
make test-eval
make test-soak
make test-install
make release-snapshot VERSION=0.1.0-rc.1 DIST=dist/0.1.0-rc.1
```

The soak starts three separate noninteractive Zsh processes against two staged
repositories, repeatedly observes and looks up both scopes while one index is
updated thirty times. It then requires fresh candidates for both final exact
staged states. It is deliberately bounded, so it is regression coverage rather
than evidence of indefinite production uptime.

| Scenario | Evidence |
| --- | --- |
| Multiple shell clients, repositories, and rapid index changes | `TestConcurrentSessionsAndRapidIndexChanges` plus `make test-soak` |
| Linked worktrees and concurrent index readers | `TestSnapshotIsolatesRepositoriesAndLinkedWorktrees`, `TestSnapshotSupportsConcurrentReaders` |
| Daemon crash/restart and malformed/truncated IPC | `tests/reliability.zsh`, daemon socket tests |
| Supersession, provider cancellation, malformed output, timeout, missing runtime | daemon/provider/evaluation tests |
| Cache corruption, expiry, and capacity | daemon cache garbage-collection tests |
| Configuration changes, cloud revocation, activity permission revocation and TTL | daemon/config/activity/cloud tests |
| Candidate cycling and partial prefixes | real-Zsh integration and command parser tests |
| Large changes, binary paths, unusual filenames, prompt-like text, and redaction | context, Git-state, and evaluation fixture tests |
| Neovim unavailable or emitter failure | headless adapter test where Neovim exists; explicit target skip otherwise |
| Install, upgrade, uninstall, checksums, and reproducible snapshots | `make test-install` |

`make bench` measures the parser composition, exact snapshot, deterministic
generation, and warm socket lookup paths. The project keeps absolute budgets of
1 ms, 25 ms, and 0.25 ms respectively for parser, background snapshot, and
warm lookup. Results on this shared Fedora host vary, so a relative regression
is investigated and recorded rather than attributed without evidence.

The synthetic evaluation corpus measures structural/evidence behavior, not
human usefulness. Ollama and OpenAI have mocked contract coverage here; no live
Ollama model or paid OpenAI credential was available for final validation.
