# Reliability and evaluation evidence

`make test` runs Go package tests, the real-Zsh interaction and daemon-restart
tests, synthetic evaluation integration, Neovim headless validation when
available, installation/release lifecycle checks, and a bounded multi-shell
soak. The targets below are useful when isolating a failure:

```zsh
make test-integration
make test-eval
make test-dogfood
make test-soak
make test-install
make release-snapshot VERSION=0.1.0-rc.1 DIST=dist/0.1.0-rc.1
make fuzz FUZZ_TIME=3s
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
| Installed plugin, isolated Zsh sessions, widget acceptance/cycling, partial intent, quote completion, invalidation, unload, and pseudo-terminal ghost-text output | `make test-dogfood` |
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

The evaluator's deterministic control now exercises the daemon's background
context, provider, grounding, ranking, temporary cache-publication, and exact
lookup path. Focused tests wait for the worker to finish before removing a
fixture and persist Git alternate-object access for historical replay, so the
same path is available to a future local-model run. It deliberately does not
replace foreground daemon/socket or visual-terminal validation. See
[EXECPLAN-LIVE-MODEL.md](EXECPLAN-LIVE-MODEL.md) for the live gate.

## RC1 cache, fuzz, and terminal evidence

The final snapshot-before-remember check removes a result when it is already
superseded. A state change can still occur after that final check and leave an
obsolete content-addressed cache file. Exact fingerprint/scope lookup prevents
it from rendering; independently, cache collection runs at daemon start and
after every write and deterministically applies the configured maximum age,
record count, and byte limit. RC1 classifies this race as safely bounded, not
eliminated. Cache GC tests cover corrupt, expired, stale, count, byte-limit,
and in-memory eviction behavior.

RC1 adds Go fuzz targets for command parsing/quoting, IPC frames, structured
provider output, declarative policy/configuration, activity events, learning
imports/remote normalization, and redaction/context bounds. Seven 3-second
single-worker campaigns completed without a crash, hang, allocation-limit
failure, or invariant violation. The corpus seeds and generated findings are
not committed.

`make test-dogfood` starts an isolated `zsh -dfi` pseudo-terminal using the
installed plugin and verifies ANSI ghost-text output after the commit buffer is
typed. The same test runs normal autosuggestions widget acceptance and cycling
in isolated real-Zsh sessions. It cannot assess terminal pixels, theme
contrast, keymap customizations, or subjective usefulness; those remain
explicit maintainer checks in [RELEASE-CHECKLIST.md](RELEASE-CHECKLIST.md).
