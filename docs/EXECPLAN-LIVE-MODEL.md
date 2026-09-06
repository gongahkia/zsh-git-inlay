# Live local-model evaluation plan

## Status

This plan starts from the RC1 assessment at `df3ce50`. The local repository had
no `ollama` executable, installed package, system unit, reachable loopback
runtime, or model manifest when first inspected on 2026-09-06. The maintainer
then approved and performed the bounded Fedora installation; this work pulled
only the three named models and used only the loopback runtime with synthetic
fixtures. No source context, cloud request, artifact, or telemetry was
uploaded by this work.

The completed development comparison rejects all three tested models for this
host and frozen product settings. The deterministic provider remains the
selected and validated default. No held-out run or prompt iteration was
appropriate after every candidate failed the development safety/operational
gate.

## Approval boundary

Fedora 43 supplied `ollama-0.9.4-4.fc43.x86_64` from the `fedora` repository.
The RPM itself is 61.0 MiB to download and 772.8 MiB installed, but the actual
approved transaction selected ROCm dependencies: DNF reported 1.4 GiB inbound
and 8 GiB installed. This is the authoritative footprint for this host; the
earlier RPM-only estimate was incomplete. The package list contains no systemd
unit, and all testing used a foreground `ollama serve` bound to
`127.0.0.1:11434`. This work did not enable or create a service.

| Candidate | Installed tag digest | Installed size | Parameters / quantization | License |
| --- | --- | ---: | --- | --- |
| `qwen2.5-coder:0.5b` | `4ff64a7f502a08b7616edb8ca0a79eb1853fc363d842b7df4b46915d11a3fb09` | 397,821,516 B | 494.03M / Q4_K_M | Apache-2.0 |
| `qwen2.5-coder:1.5b` | `d7372fd828518a4d38b1eb196c673c31a85f2ed302b3d1e406c4c2d1b64a0668` | 986,062,089 B | 1.5B / Q4_K_M | Apache-2.0 |
| `qwen2.5:0.5b` | `a8b0c51577010a279d933d14c2a8ab4b268079d44c5c8830c0a93900f1827c67` | 397,821,319 B | 494.03M / Q4_K_M | Apache-2.0 |

The installed model manifests total 1,781,704,924 bytes. Ollama reported
CPU-only inference with no compatible GPU. `ollama --version` and
`/api/version` both reported `0.0.0` despite the installed Fedora package
NEVRA above; that discrepancy is recorded, not normalized away. Tags are not
treated as immutable. The host had 7.9 GiB available RAM at initial runtime
inspection.

The upstream `install.sh` was inspected but is deliberately not used: on Linux
it modifies `/usr/local`, removes a prior runtime directory, creates a system
user/group, writes and may enable a systemd service, and may install GPU
dependencies. The contained Fedora package is the narrower reviewed method.

The exact proposed commands, **only after approval**, are:

```zsh
sudo dnf install ollama
OLLAMA_HOST=127.0.0.1:11434 ollama serve
ollama pull qwen2.5-coder:0.5b
ollama pull qwen2.5-coder:1.5b
ollama pull qwen2.5:0.5b
```

`ollama pull` contacts the model registry only to download the named weights.
`zsh-git-inlay` sends bounded synthetic fixture context only to the loopback
runtime. It makes no cloud-provider request. At the end, the maintainer may
remove only the newly downloaded weights with:

```zsh
ollama rm qwen2.5-coder:0.5b qwen2.5-coder:1.5b qwen2.5:0.5b
```

Removing the RPM with `sudo dnf remove ollama` is a separate maintainer choice;
this plan will neither remove it nor alter pre-existing `~/.ollama` data.

## Live development result and provider decision

The frozen development corpus was run through the actual daemon preparation
pipeline: staged context compilation, loopback Ollama generation, structured
decoding, conservative grounding/ranking, private cache publication, and exact
lookup. Each of the eight public synthetic development cases generated once
cold and once warm, with fallback forced to `none`. Reports were written to
private temporary directories and removed after their aggregate results were
recorded here.

| Model | Candidate count | Error rate | Timeout rate | Cold avg / p95 | Warm avg / p95 | Result |
| --- | ---: | ---: | ---: | --- | --- | --- |
| `qwen2.5-coder:0.5b` | 0 | 100% | 75% | 9,969.79 / 10,024.91 ms | 8,740.15 / 10,018.91 ms | reject |
| `qwen2.5-coder:1.5b` | 0 | 100% | 100% | 10,008.87 / 10,015.85 ms | 10,007.71 / 10,012.55 ms | reject |
| `qwen2.5:0.5b` (clean single-model rerun) | 0 | 100% | 43.75% | 9,881.10 / 10,016.40 ms | 6,624.09 / 10,006.71 ms | reject |

The 0.5B coder model produced six cold/warm timeout pairs; the remaining
attempts were invalid structured candidates or had no candidate eligible after
grounding. The 1.5B coder model timed out on all 16 calls. In the clean general
model rerun, all eight cold calls either timed out or reached the evaluator
deadline, and every non-timeout warm call was rejected as an invalid structured
candidate. No malformed response reached a prepared record, and no
deterministic fallback was used.

An earlier general-model run was excluded from selection evidence after the
loopback service became unreachable mid-run; its later fixture calls failed
with connection refusal. The termination cause is unverified: kernel logs were
not readable, while the user journal showed the foreground terminal's PTY
eventually closed. It is not labeled an OOM kill. A fresh foreground runtime
and one-model rerun produced the general-model row above.

The Ollama runner RSS samples were approximately 683 MiB for the 0.5B coder,
1.30 GiB for the 1.5B coder, and 685--854 MiB for the general 0.5B model. The
first sequential comparison retained multiple runners for the product's fixed
five-minute keep-alive and depleted available swap; therefore swap is not a
clean per-model measurement and is not used to claim the memory gate passed.
The fresh general rerun loaded one runner, but inherited nearly exhausted swap.

The opt-in `make test-live-ollama` rehearsal with `qwen2.5:0.5b` exercised the
actual isolated product daemon and Unix socket. It stopped after 12 seconds
with no prepared candidate, which is the safe expected failure for this model;
it does not establish a successful model-backed Zsh suggestion. Existing
deterministic and mocked-provider tests remain the evidence for stale rejection
and malformed-output non-rendering.

No model reached the development threshold, so held-out evaluation, three-run
repeatability, model selection, human candidate review, and prompt iteration
were deliberately not performed. Altering the frozen prompt, timeout, or
fallback to rescue an individual model would not be a defensible comparison.
The default remains `deterministic`; Ollama remains an implemented local
provider whose tested candidate models are not recommended on this host.

## Frozen protocol and acceptance criteria

Before model-specific prompt work, the frozen control is corpus `v2`, SHA-256
`99cbe770cb27d2e9e1a99f74e8d16fb6cb5b88e50f5c3cfd8a417d6287fd6361`, with
eight development and seven held-out synthetic cases. Scoring is the checked-in
structural evaluator plus the daemon's grounding/ranking result; the prompt is
`v1`, temperature is zero, context is 2,048 tokens, maximum output is 96
tokens, keep-alive is five minutes, and each generation has a 10-second
evaluator deadline. The Ollama transport enforces a 16 KiB prompt and 64 KiB
response ceiling. Evaluation forces fallback to `none`, so a deterministic
candidate cannot be mistaken for a model result.

A recommended managed local model must meet all of these predeclared gates:

1. All held-out and adversarial candidates that reach the prepared record are
   schema-valid and `GROUNDED` or `PARTIALLY_GROUNDED`; none is ungrounded or
   stale, and no malformed response reaches lookup.
2. Held-out and adversarial runs have zero unsupported issue identifiers,
   unsupported test outcomes, unsupported behavioral claims, errors, and
   timeouts; every automatic structural, daemon-grounding, and repeated-run
   check passes.
3. Three independent held-out runs have identical candidate sequences at the
   pinned generation settings. Any variance is recorded rather than discarded.
4. Warm p95 generation fits the configured 10-second background deadline,
   foreground lookup remains within the existing 0.25 ms benchmark budget, and
   a best-available process measurement shows no more than 6 GiB resident
   memory or a material swap increase on this host.
5. The model shows a documented, manually reviewed improvement over the frozen
   deterministic control in at least one of useful factual specificity,
   component identification, or repository-style adherence, without a safety
   regression. Fluency alone is not enough.

Time to first token is unavailable with the product's deliberately
non-streaming structured-response API. Cold load is measured by the first
generation; warm latency is measured by each immediately repeated generation.
Peak resident memory and CPU are sampled from the local Ollama process during
the live runs and reported as approximate measurements.

## Reproduction sequence

1. Record `ollama --version`, `ollama list`, the selected model's actual
   digest/size/quantization, `df -h`, `free -h`, and the current commit. Do not
   copy model blobs or raw prompts into the repository.
2. Run the deterministic control and each model through
   `zsh-git-inlay evaluate --fixtures --provider ollama --model <tag>` for the
   development partition. Prompt changes, if any, must be small, named, and
   recorded before rerunning development only.
3. Freeze the selected prompt, then run the held-out partition three times and
   retain private aggregate JSON/Markdown reports. Do not edit held-out
   fixtures after their first result.
4. Run the same model through a private foreground daemon with
   `provider.fallback = "none"`, then exercise prepared lookup, stage
   supersession, daemon restart, concurrent repositories, and the existing
   real-Zsh test scenarios. The corpus evaluator executes the daemon's actual
   context, provider, grounding, ranking, cache-publication, and exact-lookup
   code with a temporary private cache; it intentionally does not open a Unix
   socket. The foreground daemon check supplies the service/socket evidence:

   ```zsh
   ZSH_GIT_INLAY_LIVE_OLLAMA_MODEL=<installed-exact-tag> make test-live-ollama
   ```

   This opt-in target checks the already-running loopback runtime's installed
   model list, but never installs Ollama, pulls a model, or starts an Ollama
   service. It creates only temporary product config, cache, data, socket, and
   Git-fixture directories; it verifies the actual daemon/socket path,
   Ollama metadata, grounded prepared lookup, exact staged-state replacement,
   and recovery after a deliberately killed product daemon.
5. Re-run the normal reliability, race, static, and benchmark gates. Preserve
   all failures in the aggregate report and retain deterministic as the default
   unless every gate above passes.

## Current frozen deterministic control

The 2026-09-06 daemon-path control passed all checks. Development: 8 cases / 24
candidates, 31.49 ms cold average (p50 33.27, p95 34.75), 30.66 ms warm
average (p50 32.29, p95 33.90), zero timeout/error, 100% repeated sequence,
and 20,776,536 bytes approximate aggregate process allocation. Held-out: 7
cases / 21 candidates, 33.86 ms cold average (p50 33.74, p95 36.27), 33.34 ms
warm average (p50 33.94, p95 34.83), zero timeout/error, 100% repeated
sequence, and 20,124,376 bytes approximate aggregate allocation. These are
daemon-path control measurements, not local-model quality results.

The older RC1 direct-provider report remains historical evidence; it is not
used for model selection because it bypassed publication/ranking. No prompt
iteration or model-specific tuning occurred. The live development evidence
selects the deterministic provider by rejecting each tested Ollama model, not
by claiming a model-quality win for deterministic text.
