# Evaluation

The evaluation harness measures candidate behavior before a local model becomes
part of the default path. It runs outside ZLE and outside the daemon lookup
path. The built-in corpus is public, synthetic, versioned as `v1`, and covers
feature work, bug fixes, refactors, tests, documentation, dependencies,
renames, deletions, mixed and ambiguous changes, and staged prompt-like text.

Run the deterministic CI corpus with:

```zsh
make test-eval
zsh-git-inlay evaluate --fixtures --json
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
- unsupported issue identifiers and behavioral-claim tokens;
- candidate diversity;
- cold and warm latency, approximate allocation, timeout rate, and error rate.

The unsupported-component, issue, and behavioral-claim checks are bounded
token/evidence checks. They are not semantic proof that a change fixes,
prevents, or otherwise causes a behavior. Historical subjects are not an
unquestionable ground truth, and string similarity is deliberately not scored.
The Markdown report labels this manual-review boundary explicitly.

No private corpus directory is created or used by default. Known local
evaluation-report patterns are ignored by Git, while default reports are kept
outside the repository. Do not pass a repository working tree as
`--output-dir` when evaluating private history.
