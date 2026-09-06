# Evaluation

The evaluation harness measures candidate behavior before a local model becomes
part of the default path. It runs outside ZLE, but each command evaluation uses
the daemon's actual background context, provider, grounding, ranking, private
cache-publication, and exact-lookup code with a temporary private cache. It
does not open a Unix socket; a separate foreground-daemon rehearsal remains
necessary to validate socket service behavior. The built-in corpus is public,
synthetic, versioned as `v2`, and covers
feature work, bug fixes, refactors, tests, documentation, dependencies,
renames, deletions, mixed and ambiguous changes, partial staging, misleading
comments, shell-metacharacter filenames, untrusted issue-like identifiers, and
staged prompt-like text. Eight development cases and seven held-out cases are
separate so prompt iteration cannot be reported only on the cases used to
motivate it.

Run the deterministic CI corpus with:

```zsh
make test-eval
zsh-git-inlay evaluate --fixtures --json
zsh-git-inlay evaluate --fixtures --partition development --json
zsh-git-inlay evaluate --fixtures --partition held-out --json
```

The command writes a JSON report and a Markdown report to
`$XDG_STATE_HOME/zsh-git-inlay/evaluations` (or the private fallback
`~/.local/state/zsh-git-inlay/evaluations`). `ZSH_GIT_INLAY_STATE_DIR` is a
test/troubleshooting override. Reports and their parent directories use private
permissions. `--output-dir` is available when an explicitly chosen private
location is required.

To replay a local repository without copying its history into this repository:

```zsh
zsh-git-inlay evaluate --repo /absolute/path/to/repository --limit 50 --json
```

The runner selects non-merge commits with one parent, reads the parent tree
through Git's local object database, and applies each parent-to-commit patch to
a temporary staged index. It does not check a historical working tree into this
project and it does not write raw diffs into either report. The original commit
subject is retained only in the private local report as a review reference.
Merge commits are skipped and reported as such.

Use `--partition development` only while diagnosing a categorized failure.
Record any prompt change, then run `--partition held-out` for the reported
comparison. `--partition all` remains regression coverage; it is not a
held-out quality result. `--provider ollama --model <already-installed-model>`
first discovers the already-local model to record its tag digest and size, then
uses the daemon pipeline above. It never pulls a model and forces fallback to
`none`, so a deterministic candidate cannot be counted as model output.

## Live local-model result

On 2026-09-06, the public development partition was run through a foreground,
loopback-only Fedora Ollama runtime with the frozen `v1` prompt and 10-second
deadline. `qwen2.5-coder:0.5b`, `qwen2.5-coder:1.5b`, and `qwen2.5:0.5b` all
produced zero prepared candidates and a 100% generation-error rate. Their
timeout rates were respectively 75%, 100%, and 43.75% in the clean
single-model rerun for the general model. The detailed digests, latency,
runtime interruption, RSS samples, and rejection rationale are in
[EXECPLAN-LIVE-MODEL.md](EXECPLAN-LIVE-MODEL.md).

No prompt change, held-out run, or human preference claim follows from failed
development gating. The deterministic provider remains the default; the tested
Ollama models are not recommended on this host under the product's 10-second
background deadline.

## Metrics and interpretation

Each report records the corpus SHA-256, provider/model/runtime/quantization/
prompt metadata, local model digest and size when available, provider settings,
evaluator timeout and commit limit, OS/architecture/CPU count/Go version, and
cold/warm average, p50, and p95 generation timing. Process allocation is an
approximate measurement, not a model-memory claim.

Automatic metrics cover:

- output/schema validity;
- Conventional Commit format when a fixture declares it applicable;
- allowed scope and subject-length compliance when declared;
- changed-component identification and unsupported component names;
- unsupported issue identifiers, test-outcome claims, and behavioral-claim tokens;
- structural grounding coverage across the automatic shape/component/claim
  checks;
- actual daemon-grounding coverage for candidates published through the
  conservative ranking policy;
- exact candidate-sequence equality between each cold and immediate warm run;
- candidate diversity;
- cold and warm latency, approximate allocation, timeout rate, and error rate.

The unsupported-component, issue, and behavioral-claim checks are bounded
token/evidence checks. They are not semantic proof that a change fixes,
prevents, or otherwise causes a behavior. Historical subjects are not an
unquestionable ground truth, and string similarity is deliberately not scored.
Structural grounding coverage is likewise a report-level proxy over candidate
shape, changed components, and unsupported tokens; it is not a substitute for
the daemon's evidence-ID grounding diagnostics or semantic proof. The Markdown
report labels this manual-review boundary explicitly. A report does not prove
semantic correctness, user preference, peak model memory, CPU use,
socket-host behavior, or terminal pixels.

No private corpus directory is created or used by default. Known local
evaluation-report patterns are ignored by Git, while default reports are kept
outside the repository. Do not pass a repository working tree as
`--output-dir` when evaluating private history.

## Frozen daemon-path deterministic control

The frozen 2026-09-06 Fedora control is corpus `v2`, SHA-256
`99cbe770cb27d2e9e1a99f74e8d16fb6cb5b88e50f5c3cfd8a417d6287fd6361`, prompt
`v1`, deterministic provider, conservative grounding, `fallback = "none"`,
and a 10-second evaluator deadline. Development had 8 cases/24 candidates and
held-out had 7/21. Every automatic shape, policy, issue/test-outcome/
behavioral-claim, structural-grounding, daemon-grounding, and repeated-run
check passed in both partitions; timeout and error rates were zero.

Development averaged 31.49 ms cold (p50 33.27, p95 34.75) and 30.66 ms warm
(p50 32.29, p95 33.90), with 20,776,536 bytes approximate aggregate process
allocation. Held-out averaged 33.86 ms cold (p50 33.74, p95 36.27) and 33.34
ms warm (p50 33.94, p95 34.83), with 20,124,376 bytes approximate aggregate
allocation. These are control measurements through the daemon pipeline, not
local-model quality or model-memory evidence. The older direct-provider RC1
measurements are retained in execution history but are not comparable controls
because they bypassed publication and ranking.

A separate local-history replay of 20 eligible project commits produced 60
candidates with no generation errors or timeout. It reported 80% changed-
component/structural-proxy coverage. The four misses changed only repository
root documentation/workflow or Lua-adapter paths, for which the deterministic
candidate generator had no component scope. This is retained as a diagnostic,
not tuned away and not a model-quality score.

These results validate the harness and deterministic fallback only. They do
not establish semantic usefulness, human preference, or local-model quality.
No prompt was iterated against the held-out partition. A local-model selection
requires a recorded development comparison, three held-out runs, zero
malformed/unpublished/timeout/error results under the selected operational
timeout, no unsupported claims, manual review of factual adequacy, and an
improvement over this control. The complete acceptance criteria and approval
boundary are in [EXECPLAN-LIVE-MODEL.md](EXECPLAN-LIVE-MODEL.md). No model
satisfies that evidence standard because each tested model failed the
development operational and structured-output gates.
