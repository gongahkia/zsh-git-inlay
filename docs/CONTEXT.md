# Bounded context compiler

Context is compiled only by a daemon generation job or the administrative
`context` command. The Zsh autosuggestion strategy and its lookup helper never
read a patch, staged blob, repository history, or provider endpoint.

The compiler reads only Git's staged index and bounded Git metadata. Its fixed
sources are changed paths/statuses, a selected staged patch, nearby declarations,
root package manifests, recent subjects, recent path history, a staged
`.zsh-git-inlay.toml`, the symbolic branch, and a bounded branch issue ID.
There is no filesystem or repository-content walk and no working-tree content
read. Git history queries stop after their bounded result, output, and two-second
compiler time limits. Source priority is manifest, source code, ordinary text,
then documentation; ties sort by path.
Only the first 64 changed paths, 12 patch paths, 8 symbol blobs, and 8 history
paths can be considered.

`vendor`, `node_modules`, build/dist/generated paths, lockfiles, and recognized
binary extensions remain visible only as bounded changed-path metadata. Their
content is not included. Git's binary patch marker contains no blob data. The
compiler reads file blobs through `git show :path`, never through the working
tree, so unstaged content is excluded.

Each source has an enforced byte budget: paths 4 KiB, patch 12 KiB, symbols
6 KiB, manifests 6 KiB, recent subjects 2 KiB, path history 2 KiB, convention
2 KiB, branch 256 bytes, and issue ID 128 bytes. The deterministic preview has
a 32 KiB aggregate source budget; the Ollama preview has a 12 KiB aggregate
budget, leaving room below Ollama's 16 KiB prompt limit. Command output is
streamed into capped buffers, so an oversized diff does not create an
unbounded in-process output buffer.

Before provider submission, common private-key blocks, AWS/GitHub token forms,
credential assignments, and bearer tokens are redacted. This is a defense in
depth filter, not a claim that arbitrary secrets can be recognized. All
repository-derived fields are wrapped as untrusted data; the provider system
instruction says they cannot override output, evidence, or execution rules.

Inspect the selected sources without printing their content:

```zsh
zsh-git-inlay context --cwd .
zsh-git-inlay context --cwd . --json
zsh-git-inlay context --cwd . --provider ollama
```

The output includes source inclusion/exclusion reasons, byte use, truncation,
redaction counts, provider-specific budget, and fingerprints. It intentionally
omits the compiled data and prompt. `context` requires staged changes just like
candidate preparation.

The cache identity includes a versioned context fingerprint derived from the
repository/worktree identity, HEAD or unborn ref, exact index tree, branch,
relevant configuration, and compiler version. The cache record retains that
fingerprint for diagnostics; the fast lookup uses the encompassing staged-state
fingerprint and does not compile context.
