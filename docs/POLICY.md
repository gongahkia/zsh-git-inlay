# Repository message policy

`.zsh-git-inlay.toml` is a small declarative repository message-policy file. It
cannot choose a provider, endpoint, credential, cloud grant, permission,
activity capture setting, output capture setting, hook, command, or redaction
rule. Unknown keys are rejected before candidate preparation.

```toml
[commit]
convention = "conventional"
types = ["feat", "fix", "test", "docs"]
scopes = ["api", "cli"]
scope_paths = ["internal/api=api", "cmd=cli"]
line_length = 72
capitalization = "lower" # or "sentence"
body = "optional"         # forbid, optional, or required
```

The only currently supported convention is `conventional`; it keeps the
existing candidate schema and safety floor intact. `types` and `scopes` are
allowlists when non-empty. A scope path is a relative repository prefix mapped
to a declared scope. The longest matching rule is used per changed path; a
candidate scope must match one of the inferred scopes when any path rule
applies. A total candidate line longer than `line_length` is rejected.

`lower` requires a lowercase first letter in the subject; `sentence` requires
an uppercase first letter. Uppercase subject text remains shell-safe under the
existing printable-message floor. Normal suggestions are subject-only, so a
repository that requires a body intentionally gets no normal candidate. The
user may instead invoke `zsh-git-inlay compose` for an already prepared,
grounded candidate: it proposes a policy-wrapped body, opens the configured
editor, rechecks the exact staged state, and never creates a commit. A `forbid`
policy rejects composition. See [COMPOSE.md](COMPOSE.md).

Effective precedence is fixed:

```text
built-in message and shell-safety floor
  -> user-global provider and privacy grants
  -> repository message convention and scopes
  -> safe invocation quoting/prefix constraints
```

Repository policy cannot modify a higher layer. Its content participates in the
exact candidate fingerprint, and `zsh-git-inlay explain --json` reports the
effective policy plus its provenance. A malformed policy produces an
`unsupported` staged state rather than executing or partially applying it.
