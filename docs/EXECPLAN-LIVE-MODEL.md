# Live local-model evaluation plan

## Status

This plan starts from the RC1 assessment at `df3ce50`. The local repository had
no `ollama` executable, installed package, system unit, reachable loopback
runtime, or model manifest when inspected on 2026-09-06. The existing
`~/.ollama` directory is not treated as product data and its contents are not
read by this plan. No model, runtime, source context, cloud request, artifact,
or telemetry was uploaded by this work.

The live evaluation is blocked pending explicit maintainer approval for the
bounded installation and downloads below. The deterministic provider remains
the selected default until a model satisfies every acceptance criterion.

## Approval boundary

Fedora 43 currently offers `ollama-0.9.4-4.fc43.x86_64` from the `fedora`
repository: 61.0 MiB download and 772.8 MiB installed. Its package file list
contains the executable and local libraries, but no systemd unit. The proposed
runtime is therefore a foreground, per-evaluation `ollama serve` bound to
`127.0.0.1:11434`; this plan does not enable or create a service.

| Candidate | Official current tag digest | Download | Parameters / quantization | License |
| --- | --- | ---: | --- | --- |
| `qwen2.5-coder:0.5b` | `4ff64a7f502a` | 398 MB | 494M / Q4_K_M | Apache-2.0 |
| `qwen2.5-coder:1.5b` | `d7372fd82851` | 986 MB | 1.54B / Q4_K_M | Apache-2.0 |
| `qwen2.5:0.5b` | `a8b0c5157701` | 398 MB | 494M / Q4_K_M | Apache-2.0 |

The model downloads total 1,782 MB (about 1.8 GB) and are stored under the
user's Ollama data directory. The live run records the actually installed
digest, size, quantization, and CLI version; tags are not treated as immutable.
The host had 728 GiB free disk and 7.9 GiB available RAM at inspection time.

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

## Reproduction sequence after approval

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
iteration, model-specific tuning, live Ollama request, or provider selection
has occurred in this plan.
