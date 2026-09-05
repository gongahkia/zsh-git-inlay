# Evaluation

The evaluation harness measures candidate behavior before a local model becomes
part of the default path. It runs outside ZLE and outside the daemon lookup
path. The built-in corpus is public, synthetic, versioned as `v2`, and covers
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
uses the same bounded staged context compiler as the daemon and never pulls a
model. This host has no Ollama runtime/model, so completed RC1 runs evaluate
only the deterministic provider; no model-comparison claim follows from that
absence.

## Metrics and interpretation

Each report records provider/model/runtime/quantization/prompt metadata,
provider settings, evaluator timeout and commit limit, OS/architecture/CPU
count/Go version, and cold/warm generation timing. Process allocation is an
approximate measurement, not a model-memory claim.

Automatic metrics cover:

- output/schema validity;
- Conventional Commit format when a fixture declares it applicable;
- allowed scope and subject-length compliance when declared;
- changed-component identification and unsupported component names;
- unsupported issue identifiers, test-outcome claims, and behavioral-claim tokens;
- structural grounding coverage across the automatic shape/component/claim
  checks;
- candidate diversity;
- cold and warm latency, approximate allocation, timeout rate, and error rate.

The unsupported-component, issue, and behavioral-claim checks are bounded
token/evidence checks. They are not semantic proof that a change fixes,
prevents, or otherwise causes a behavior. Historical subjects are not an
unquestionable ground truth, and string similarity is deliberately not scored.
Structural grounding coverage is likewise a report-level proxy over candidate
shape, changed components, and unsupported tokens; it is not a substitute for
the daemon's evidence-ID grounding diagnostics or semantic proof. The Markdown
report labels this manual-review boundary explicitly.

No private corpus directory is created or used by default. Known local
evaluation-report patterns are ignored by Git, while default reports are kept
outside the repository. Do not pass a repository working tree as
`--output-dir` when evaluating private history.

## RC1 deterministic evidence

The 2026-09-05 Fedora RC1 run used corpus `v2` and the built-in deterministic
provider. Development had 8 cases/24 candidates and held-out had 7/21. Every
applicable automatic check passed in both partitions: output shape,
conventional format, declared scope and length, changed/allowed component,
issue/test-outcome/behavioral-claim rejection, and structural-grounding proxy.
The held-out run averaged 1.784 ms cold, 1.726 ms warm, and 326,128 bytes of
process allocation per case; its timeout and error rates were zero.

A separate local-history replay of 20 eligible project commits produced 60
candidates with no generation errors or timeout. It reported 80% changed-
component/structural-proxy coverage. The four misses changed only repository
root documentation/workflow or Lua-adapter paths, for which the deterministic
candidate generator had no component scope. This is retained as a diagnostic,
not tuned away and not a model-quality score.

These results validate the harness and deterministic fallback only. They do
not establish semantic usefulness, human preference, or local-model quality.
No prompt was iterated against the held-out partition in RC1. A local-model
selection requires a recorded development comparison followed by a held-out
run, zero malformed/timeout/error results under the selected operational
timeout, and manual review of grounded factual adequacy. No model satisfies
that evidence standard yet because no Ollama runtime/model is installed.
