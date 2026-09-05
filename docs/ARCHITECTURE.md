# Architecture

## Runtime path

`zsh-git-inlay.plugin.zsh` is the only interactive component. After verifying that `zsh-autosuggestions` is already loaded, it prepends `git-inlay` to `ZSH_AUTOSUGGEST_STRATEGY`; yielding from that strategy lets the user's history and completion strategies continue normally. It does not define `git`, use `eval`, render text, scan a diff, generate candidates, or access a network.

On `precmd`, the plugin starts `zsh-git-inlay daemon serve --observe "$PWD"` in the background when the daemon is absent. Startup is never awaited. The autosuggestion strategy is asynchronous on supported autosuggestions/Zsh versions. Its helper invocation parses a bounded buffer, obtains the current index-tree identity with Git plumbing, then performs a short bounded local Unix-socket lookup. A cache miss, daemon startup, stale state, no staged content, conflict, or unsupported command yields immediately. The helper never requests generation from this path.

The cycle widget increments per-shell selection state and invokes autosuggestions' ordinary asynchronous fetch path with `--no-start`; it cannot launch a daemon or generate work. A `precmd` reset makes the first candidate primary after normal stage/unstage commands return to the prompt. Candidate selection is modulo the deterministic candidate count and wraps after the last candidate.

## Exact fingerprint

For a repository worktree, the engine calculates:

```text
SHA-256(length-prefix(
  SHA-256(canonical Git common directory),
  SHA-256(canonical worktree Git directory),
  HEAD OID or "unborn:<symbolic ref>",
  git write-tree OID,
  symbolic branch or "detached",
  provider and context compiler versions,
  SHA-256(global relevant config version + repository config version),
  SHA-256(versioned context identity over the preceding context inputs)
))
```

`git write-tree` describes exact index content, including modes and renames, without reading the working tree or a staged diff. Because Git can add a cache-tree extension to its input index, the engine gives it a secure private copy of the bounded real index rather than the real index itself. The resulting tree is compared with `HEAD^{tree}` (or indexed entries on an unborn branch), so unstaged edits retain the same candidate identity. `git ls-files -u` rejects unresolved indexes explicitly after a failed tree write. This preserves concurrent reader behavior and never modifies the real index. Git may create an immutable, unreferenced tree object for an unseen index tree; it creates no ref, index entry, or commit, and normal Git garbage collection can prune it. An unborn branch records its symbolic HEAD ref. The common Git directory identifies a repository while the worktree Git directory prevents linked worktrees from sharing candidates.

This uses Git's index representation, not modification timestamps. It is an index operation rather than a repository-wide diff scan; the benchmark records its cost separately. It still starts a short-lived Git helper process, so it belongs in autosuggestions' async strategy process, not a synchronous custom ZLE widget.

## Daemon, IPC, and cache

The daemon is one process per user runtime location. Its listener is a `0600` Unix socket in a `0700` directory under `XDG_RUNTIME_DIR`, with a private XDG cache fallback. A pre-existing live socket is left alone; a non-socket is refused instead of replaced. Cache directories and records are private (`0700` and `0600`).

IPC is a length-prefixed, versioned JSON request/reply protocol with a 16 KiB request and 64 KiB reply ceiling. Requests have a deadline. Invalid sizes, invalid JSON, unknown operations, incompatible versions, and malformed scope identifiers receive explicit rejection. The daemon has no network protocol and accepts only the local Unix socket.

An `observe` request snapshots the repository, bounds active repositories, and deduplicates a job by fingerprint. A new fingerprint for the same repository/worktree cancels the old job. Generation concurrency is bounded. Before publishing, the worker snapshots the same worktree again and discards a mismatched result. Candidate records are written to a private temporary file, synced, closed, and atomically renamed into a content-addressed cache file. A cold daemon loads a matching on-disk record rather than regenerating it.

Candidate storage is bounded by a validated user configuration: 512 records,
32 MiB, and seven days by default. Startup and every publication deterministically
remove malformed, expired, and oldest over-capacity records; lookup also removes
an expired record. `status --json` reports current cache size and cumulative
expired, corrupt, capacity, and stale-result removals without exposing candidate
text. The daemon's final post-publication snapshot removes a result that became
superseded during publication.

Git exposes no non-blocking transaction that can hold an index stable between a final `write-tree` check and cache rename without interfering with ordinary Git. An index update in the remaining tiny interval can leave an obsolete content-addressed record, but bounded retention and deterministic garbage collection contain that storage. It cannot become a stale display because every lookup recomputes the exact current fingerprint and requires matching repository/worktree IDs; the next observe schedules the new state. Tests prove pre-publication supersession rejection, post-publication stale discard, lookup-time stale rejection, and bounded cache retention.

The default idle timeout is 15 minutes. The daemon exits only when it has been idle and has no jobs, and the next background observe starts it again. It never modifies the Git index or creates a commit.

Provider generation also stays behind the daemon boundary. Before invoking a
provider, the daemon compiles bounded staged-only context with inspectable
source and truncation reasons. The context fingerprint is part of the candidate
identity and the record. Repository-derived content is redacted and marked
untrusted before provider submission. The selected local provider returns an
untrusted structured response; the daemon validates it, converts it to the
existing shell-safe candidate form, and applies the same pre/post-publication
fingerprint checks. The strategy and lookup paths do not compile context,
invoke a provider, or wait for inference.

## Prototype provider and parser

The deterministic provider invokes `git diff --cached --name-status -z
--find-renames`, bounds metadata to 64 KiB, and creates three ordered,
deterministic conventional-style messages. It deliberately ignores compiled
source context and remains the test oracle. Ollama can instead receive the
bounded context compiler output described in [CONTEXT.md](CONTEXT.md).

The parser accepts only the stated canonical command forms, including `command git commit`, `-m`, `--message`, `-am`, flags before the message, repeated whitespace, and incomplete quote states. It rejects non-end cursors, `--amend`, `--fixup`, `--squash`, other Git commands, shell operators/substitutions, and completed message arguments. A candidate must extend the user prefix. Empty messages receive single quotes; open single or double quotes receive their matching closing quote; an unquoted typed prefix receives a shell-escaped continuation. Generated candidates are restricted to a conservative printable character set before this composition.
