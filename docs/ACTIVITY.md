# Local activity protocol

Activity is optional local evidence for the daemon. It is disabled by default,
has no network or telemetry path, and is collected by the Zsh plugin only after
the user grant. Future Neovim producers use the same protocol. Neither changes
the normal commit-message renderer or command execution path.

Only a user command can change the durable activity grant. The private record
is `$XDG_DATA_HOME/zsh-git-inlay/permissions.json` (or its data-directory
override), written `0600` beneath a private `0700` directory. A missing,
malformed, non-private, or non-user-owned record is disabled rather than
trusted.

```zsh
zsh-git-inlay permissions
zsh-git-inlay permissions enable activity
zsh-git-inlay permissions revoke activity
```

The daemon reloads that grant before every event and every activity-derived
context lookup. Revoking it clears the daemon's in-memory activity state, so a
running daemon does not need to restart. Output capture is not implemented and
has no permission grant in this milestone; it will require a separate explicit
capability if added.

The user-global configuration may bound retention, but cannot grant activity
permission:

```toml
[activity]
retention = "30m" # 1m through 24h
max_events = 256  # 1 through 4096 per repository/worktree
```

The store also holds at most the configured active-repository count (32 by
default). It is memory-only: it writes neither events nor derived signals to
disk. Repository `.zsh-git-inlay.toml` accepts no activity keys, so repository
content cannot enable collection or weaken these limits.

## Event protocol

The owner-only daemon socket accepts a length-prefixed versioned JSON `event`
request. The existing socket is `0600` inside an owner-private runtime
directory. Before accepting an event, the daemon snapshots the request's
working directory and requires the event's repository and worktree IDs to
match that local Git identity. A producer cannot select a different scope by
putting arbitrary IDs in its payload.

Every event has this shape:

```json
{
  "schema_version": 1,
  "repository_id": "sha256-hex-id",
  "worktree_id": "sha256-hex-id",
  "source": "shell",
  "kind": "git.index_changed",
  "timestamp": "2026-09-05T12:00:00Z",
  "data": {"class": "git"},
  "sensitivity": "private"
}
```

Sources are `shell`, `editor`, or `integration`. The allowlisted kinds are
`shell.command_started`, `shell.command_finished`, `git.index_changed`,
`git.head_changed`, `git.commit_completed`, `editor.file_opened`,
`editor.file_saved`, `lsp.diagnostics_changed`, `build.completed`, and
`test.completed`. Each payload has at most 12 string fields; keys and values
are constrained, values are at most 256 bytes, and the event timestamp must be
near the local clock. Unknown schema versions, kinds, sources, oversized data,
invalid identities, and malformed socket frames are rejected without stopping
the daemon.

The Neovim adapter is the sole editor producer in this version. It sends only a
buffer path, session ID, and capped diagnostic severity counts; see
[NEOVIM.md](NEOVIM.md).

Events expire by TTL and are capped per repository/worktree. The administrative
view is scoped to the current repository:

```zsh
zsh-git-inlay activity inspect --cwd .
zsh-git-inlay activity inspect --cwd . --json
zsh-git-inlay activity clear --cwd .
```

Inspection shows accepted, already-redacted events and aggregate rejection
reasons. Rejected payload text is not retained merely to make it inspectable.
`clear` deletes only the selected repository/worktree's memory state.

## Zsh collection and rejected output capture

The plugin's `preexec` hook classifies a command in memory and sends only a
fixed class name. Its `precmd` hook sends the exit status and duration for that
same recognized command. It recognizes Git commit/index commands and common
Go, Cargo, pytest, Make, npm, pnpm, and Yarn test/build forms. Command text,
arguments, environment, standard input, and output are not event fields. A
successful `git commit` marks the following background state check as a commit;
that check emits `git.commit_completed` only when it also observed a real HEAD
transition. It emits `git.index_changed` or `git.head_changed` only on a real
transition. Recognized test and build commands also emit `test.completed` or
`build.completed` with the same bounded status and duration fields, including
when the command fails.

These hook-side calls are backgrounded. The event client checks the durable
grant before taking a Git snapshot, and any client, socket, or daemon failure
is ignored by the hook, so ordinary command execution and prompt rendering do
not wait on collection. The normal autosuggestion lookup remains unchanged.

Transparent stdout/stderr capture is deliberately rejected: wrapping arbitrary
commands, redirecting terminal streams, or intercepting the terminal would
alter command semantics and expose secrets. This milestone implements no output
permission and no output capture. A future explicit tool-specific output path
must add its own separate capability and preserve the bounded
input/redaction/error-region/relevance/derived-signal sequence before it can
submit an event.

## Inference boundary

Before retention, common credential names and token forms are redacted; events
classified `secret` retain no structured data. This is a defense-in-depth
filter, not a claim that arbitrary secrets can be recognized. The context
compiler never receives retained event data. It receives at most ten stable
allowlisted `kind=count` signals for the same repository/worktree, under a 512
byte source budget. Those signals are again filtered against the fixed kind
allowlist and wrapped as untrusted data before provider submission.

The candidate record contains only a signal-set digest and those bounded kind
counts. The daemon compares that digest on observe and warm lookup. A changed,
cleared, revoked, or expired signal set makes the record `activity_stale`, so
activity-derived candidates cannot render after their evidence is gone. The
Zsh strategy still performs only its normal bounded local socket lookup; it
does not read activity data, compile context, or invoke a provider.
