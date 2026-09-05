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

Optional activity uses the same owner-only socket, but the daemon first derives
the repository/worktree identity from the supplied local working directory and
rejects an event whose payload claims another scope. Activity starts disabled,
reloads the user-private grant for each event and signal lookup, and remains
bounded memory only. The context compiler receives only a small fixed-kind
count representation—never event data—and the cached candidate records retain
only its digest. A permission revoke, clear, or TTL expiry changes that digest,
so lookup returns `activity_stale` rather than rendering a candidate generated
under prior activity evidence. [ACTIVITY.md](ACTIVITY.md) documents the schema,
redaction, retention, and administrative controls.

When activity is granted, Zsh `preexec`/`precmd` hooks operate outside the ZLE
strategy and asynchronously send only a fixed recognized command class, exit
status, and duration. A separate asynchronous daemon operation compares actual
Git index-tree and HEAD values before emitting Git transition events. The hooks
never retain command arguments or redirect terminal output; failures are
ignored. Transparent command-output capture is rejected because it would alter
normal command or terminal semantics.

The optional Neovim Lua module uses the same emitter for file and diagnostic
count events. It has no provider, renderer, buffer-content reader, or context
store. Its callbacks are asynchronous and only start after explicit Lua setup;
[NEOVIM.md](NEOVIM.md) documents its bounded fields and health check.

Provider generation also stays behind the daemon boundary. Before invoking a
provider, the daemon compiles bounded staged-only context with inspectable
source and truncation reasons. The context fingerprint is part of the candidate
identity and the record. Repository-derived content is redacted and marked
untrusted before provider submission. The selected local provider returns an
untrusted structured response; the daemon validates it, converts it to the
existing shell-safe candidate form, and applies the same pre/post-publication
fingerprint checks. The strategy and lookup paths do not compile context,
invoke a provider, or wait for inference.

The OpenAI transport is an explicit exception to local-only inference, not a
fallback route. A user-private provider-specific grant selects a complete set
of versioned context classes. The daemon maps that grant to an already-redacted
and relevance-filtered subset of compiled sources. Existing staged cache
identity includes provider/model configuration and prompt/compiler version;
cloud provenance hashes the selected class policy, staged context identity, and
selected prompt. It then rechecks both grant and exact staged identity before
each HTTP attempt. It sends a non-streaming structured request with
`store: false` and permits only one retry, after another staged-state check.
No grant, changed/revoked grant, missing credential, cancellation, outage, or
malformed response produces no cloud candidate. Lookup reloads the grant before
serving a cloud record, so revocation cannot render an old cloud-derived result.
The Zsh strategy still has no network operation. [CLOUD.md](CLOUD.md) documents
the preview and controls.

After a provider response passes its structural schema, grounding code checks
its evidence references against the compiled staged context and ranks valid
messages before publication. It records grounding states and individual
deterministic versus heuristic checks alongside the cache record for `explain`.
Unsupported evidence, issue, test, fix, or behavioral claims are rejected or
demoted according to the user-global ambiguity policy. Recent subject type and
the private local learning profile are additional style ranking signals. The
profile is read during background generation, after grounding and repository
policy, and its applied bounded reasons are retained in the candidate record
for `explain`; it adds no operation to the strategy or warm lookup path. See
[LEARNING.md](LEARNING.md).

Repository policy is loaded as strict declarative data before grounding. It can
restrict message types/scopes, infer scopes from bounded changed paths, apply
line length and capitalization rules, and state a body preference. It cannot
grant a provider or privacy capability. Its validated content is already part
of the staged fingerprint; the published record retains the effective policy
for diagnostics. [POLICY.md](POLICY.md) documents the fixed security and
configuration precedence.

## Prototype provider and parser

The deterministic provider invokes `git diff --cached --name-status -z
--find-renames`, bounds metadata to 64 KiB, and creates three ordered,
deterministic conventional-style messages. It deliberately ignores compiled
source context and remains the test oracle. Ollama can instead receive the
bounded context compiler output described in [CONTEXT.md](CONTEXT.md).

The parser accepts only the stated canonical command forms, including `command git commit`, `-m`, `--message`, `-am`, flags before the message, repeated whitespace, and incomplete quote states. It rejects non-end cursors, `--amend`, `--fixup`, `--squash`, other Git commands, shell operators/substitutions, and completed message arguments. A candidate must extend the user prefix. Empty messages receive single quotes; open single or double quotes receive their matching closing quote; an unquoted typed prefix receives a shell-escaped continuation. Generated candidates are restricted to a conservative printable character set before this composition. When a typed prefix matches a later prepared candidate, selection reranks the existing matching subset in its original order; it has no daemon, context, generation, or questionnaire operation.

`zsh-git-inlay compose` is outside the strategy path. It looks up one already
prepared candidate by the current exact fingerprint: a normally grounded
candidate, or one withheld solely because a required-body policy needs compose
to complete it. The latter never reaches normal rendering. Compose creates an
owner-only temporary message file and invokes only a resolved executable plus
literal editor arguments—never a shell. Its generated body is a bounded,
wrapped list of staged status/path evidence, with grounding retained only for
that generated content. After the editor exits it reads only the same private
regular file, snapshots the staged state again, and rejects a mismatch or
policy-invalid result while retaining the file for the user. A successful
compose command only reports that file path; it neither stages content nor
invokes `git commit`. [COMPOSE.md](COMPOSE.md) gives the user-facing details.
