# Explicit cloud provider grants

Cloud inference is opt-in, default-deny, and never an automatic fallback. The
only current adapter is OpenAI's Responses API. The default provider remains
deterministic; selecting OpenAI without both a model setting and an explicit
grant produces no suggestion rather than contacting a service or substituting
a local provider.

No live request was made during V1 development. Contract coverage uses a local
mock HTTP server only. The adapter follows the documented
[Responses API](https://platform.openai.com/docs/api-reference/responses) with
Bearer authentication, a strict JSON schema response, `stream: false`, and
`store: false`. `store: false` is an explicit request setting, not a claim about
all provider-side data handling; review the provider's current data controls
before granting repository context.

## Configuration and credentials

The user-global configuration can select the adapter but cannot contain a
credential or endpoint:

```toml
[provider]
name = "openai"
model = "gpt-5"
timeout = "8s"
fallback = "none"
```

At request time only, after the daemon has validated the grant and current
staged fingerprint, the adapter reads `OPENAI_API_KEY` from its process
environment. It does not write that value to configuration, prompts, cache
records, diagnostics, provider metadata, repository policy, or learning
profiles. A missing key makes generation fail closed without an HTTP request.

## Grant and preview

Grants are user-global, provider-specific records at
`$XDG_DATA_HOME/zsh-git-inlay/cloud-grants.json` (or the private data-path
override). The file is owner-only `0600` inside a private `0700` directory and
has no repository identifier, source text, endpoint, or credential. Repository
configuration has no grammar for these permissions.

Grant the complete desired set at once. `--confirm` and replacement semantics
mean adding a new class requires a fresh explicit consent command; one
provider's grant cannot authorize another.

```zsh
zsh-git-inlay cloud status --json
zsh-git-inlay cloud preview --provider openai --cwd . --json
zsh-git-inlay cloud grant openai --classes staged_diff,repository_context --confirm
zsh-git-inlay cloud revoke openai
```

`preview` shows source categories, byte counts, truncation, and redaction
counts, never source content. With no grant it reports that nothing would be
sent. `grant` and `revoke` notify a running daemon to remove only that
provider's cloud-derived cache records; every later lookup also reloads the
private grant, so a missed notification fails closed.

| Context class | Eligible bounded compiler sources |
| --- | --- |
| `staged_diff` | staged paths and relevance-selected staged patch |
| `repository_context` | nearby declarations, root manifests, staged convention |
| `history` | recent subjects and selected-path history |
| `branch_identifiers` | symbolic branch and bounded issue identifier |
| `activity` | consented event-kind/count signals only |
| `output_excerpts` | no source currently exists; transparent output capture is unsupported |

The compiler performs staged-only selection, generated/vendor/lockfile
exclusion, per-source and aggregate byte bounds, secret-pattern redaction, and
untrusted-data framing before cloud selection. A class grants only the mapped
selected sources, not an unbounded repository read. Existing generic context
inspection remains content-free; cloud preview shows the smaller effective
selection.

## Request lifecycle and cache identity

Cloud requests run only in a daemon background job. Before every HTTP attempt,
the adapter requires a current grant and exact staged-state proof. It uses the
configured timeout, permits at most one retry for transport failures or 408,
409, 429, and 5xx responses, and checks the staged fingerprint before retrying.
Cancellation or a superseded/revoked state stops the request/retry path. An
outage changes neither Git nor Zsh behavior: the normal ghost-text path simply
has no newly prepared cloud candidate.

The existing staged-cache identity includes provider/model configuration and
prompt/compiler version; cloud provenance additionally hashes the provider,
selected ordered context classes, staged context identity, and redacted selected
prompt. Records retain no selected text or credential. A record can render only
while its current provider grant exactly matches that stored class set; granting
additional context, replacing a grant, or revoking it invalidates the old
record.

## Validation boundary

Mock tests cover the Responses request shape, Bearer header handling without
serializing the key, `store: false`, strict structured output, denial before
any request, missing credentials, cancellation, one-retry bound,
supersession-before-retry, selected-source redaction, per-provider private
grants, CLI preview, and immediate cached-result rejection after revocation.
They do not validate a live account, billing, provider availability, or model
quality. No credential was used and no repository content was transmitted.
