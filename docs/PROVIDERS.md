# Providers

## Implemented in Milestone 2

The daemon has a small provider boundary with three implementations:

- `deterministic` is the default and needs no model runtime. It remains the
  test oracle and conservative fallback.
- `ollama` calls only `http://127.0.0.1:11434`. It never downloads or pulls a
  model, and it rejects non-loopback endpoints.
- `openai` is a default-deny cloud adapter. It can run only after a separate
  user-private `openai` context grant and exact staged-state revalidation; it
  has no automatic fallback.

Ollama uses its documented local `/api/tags`, `/api/show`, and non-streaming
`/api/generate` endpoints. Generation requests use a JSON schema, bounded
staged-only context input, a 16 KiB prompt limit, a 64 KiB response limit, a
configured timeout, and cancellation from a superseded daemon job. Model output
is untrusted: strict JSON parsing, candidate-count limits, structural field
checks, evidence identifier checks, and existing shell-message safety checks
all run before publication. See the [Ollama generate API](https://docs.ollama.com/api/generate), [model listing API](https://docs.ollama.com/api/tags), and [structured-output guidance](https://docs.ollama.com/capabilities/structured-outputs).

`doctor --json` probes only that loopback endpoint and reports reachability,
locally installed model names, and configured-model details when available. It
does not pull, run, or contact a remote model service.

```toml
[provider]
name = "deterministic" # or "ollama" or "openai"
# model = "qwen2.5-coder:0.5b" # required when name = "ollama" or "openai"
timeout = "8s"
fallback = "deterministic" # or "none"
```

Fallback is an explicit user configuration policy for local providers. With `fallback =
"deterministic"`, an unavailable, malformed, cancelled, or failed Ollama
request falls back to deterministic generation in the daemon. With `none`, the
failed generation produces no candidate. `openai` requires `fallback = "none"`;
there is no local-to-cloud or cloud-to-local automatic fallback.

## Explicit OpenAI cloud adapter

`openai` uses the documented [Responses API](https://platform.openai.com/docs/api-reference/responses)
with a Bearer key, non-streaming strict JSON schema output, and `store: false`.
The endpoint is fixed in the adapter; configuration has no endpoint or
credential key. After authorization and only at request time, the adapter reads
`OPENAI_API_KEY`. A missing key, missing/invalid grant, revocation, timeout,
network failure, malformed response, or stale state publishes no cloud
candidate and does not affect ordinary Git or Zsh behavior.

```toml
[provider]
name = "openai"
model = "gpt-5"
timeout = "8s"
fallback = "none"
```

Cloud grants are not provider configuration and cannot appear in repository
policy. The user must preview and explicitly replace the complete selected
class set with `--confirm`; every provider has an independent grant. The
context compiler's redaction/relevance pass precedes selection and transfer.
`cloud preview --provider openai --cwd . --json` exposes categories and bounds,
never source content. See [CLOUD.md](CLOUD.md) for the class mapping, cache
identity, revocation behavior, and live-validation limitation.

Provider name, configured model, timeout, fallback policy, and prompt version
are covered by the global configuration version incorporated into the staged
fingerprint. A versioned context fingerprint adds exact index, HEAD/unborn
state, branch, worktree, and compiler identity; it is retained with each cache
record. Changing this configuration or context input therefore creates a new
exact candidate identity; old records cannot serve the new configuration.
Replacing an Ollama tag in place without a
configuration change is not detected by the current cache identity. [Inference]
This is safe for staged-state isolation but means mutable model tags should not
be treated as immutable model versions; selecting an immutable model reference
or changing configuration is required to invalidate such a cache entry.

## Validation status

Mock HTTP tests cover model enumeration, configured-model inspection,
non-streaming schema requests, malformed and trailing structured output,
cancellation, loopback enforcement, explicit deterministic fallback, and
superseded-state publication checks. Context compiler tests additionally cover
redaction, prompt-injection framing, staged-only reads, deterministic source
selection, and per-source/aggregate limits. Ollama is not installed on the current
Fedora host, so live-server/model validation and a comparison against
Qwen2.5-Coder 0.5B are unavailable. No model was downloaded. Deterministic
remains the selected default until a local model passes the evaluation gate.

OpenAI mock tests cover no-request-before-grant/current-state proof, key
non-serialization, Responses request/structured-output contract, cancellation,
one-retry cap, no retry after staged supersession, selected/redacted source
transfer, per-provider grant isolation, preview, and cached-result rejection
after revocation. No OpenAI key, live account, or live provider request was
used, so live compatibility, billing, availability, and model-quality results
are unavailable.

## Managed runtime status

`zsh-git-inlay model status` reports only the private managed-data directory and
installed versions. `model install --confirm` requires an explicit confirmation
even when an authenticated manifest becomes available. The installer design
uses HTTPS-only artifacts, pinned SHA-256 checksums, bounded resume files,
private XDG data storage, an atomic staging-to-install rename, current-version
rollback, and confirmation-gated uninstall. It never touches Ollama models.

The current project deliberately bundles no runtime/model manifest: a pinned
manifest must authenticate both a specific `llama.cpp` runtime artifact and a
model license/digest, and this project has neither a release-signing authority
nor an authenticated model-distribution authority. The `llama.cpp` codebase is
MIT-licensed, but its own release guidance describes evolving release channels
and development artifacts rather than a stable third-party distribution contract.
See [its license](https://github.com/ggml-org/llama.cpp/blob/master/LICENSE) and
[release process](https://github.com/ggml-org/llama.cpp/blob/master/docs/release.md).
Consequently `model install --confirm` reports that blocker without downloading.
Mock fixtures verify the acquisition mechanics; they are not a claim that a
production runtime/model distribution is available.
