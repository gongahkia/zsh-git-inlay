# Secondary commit-body composition

`zsh-git-inlay compose` is an optional administrative editor workflow. The
ordinary product remains one single-line Zsh autosuggestion; composing a body
does not alter the ZLE strategy, create a commit, stage content, or replace
normal `git commit` usage.

The command requires an already prepared candidate for the current exact
staged fingerprint. It accepts only a normally grounded candidate, or the
special candidate withheld solely because repository policy requires a body;
every subject/evidence check must otherwise pass. That withheld input never
renders as ghost text. Compose builds a small proposal with the subject and a
wrapped `Staged paths:` list whose status/path facts are individually tied to
the current staged evidence. It intentionally does not invent behavior,
motivation, test, or outcome claims. The proposal is written to an owner-only
`0600` temporary file and opened through the configured Git editor:

```zsh
zsh-git-inlay compose --cwd .
zsh-git-inlay compose --cwd . --candidate 1 --json
```

The command accepts `GIT_EDITOR` when set; otherwise it consults only the
user-global `core.editor`, then `VISUAL`, `EDITOR`, and the `vi` fallback. It
does not consult repository-local `core.editor`. It invokes only a simple
executable plus literal arguments. Quoted, shell-composed, and shell
executables are rejected rather than evaluated. A normal editor command such
as `nvim -f` is supported. Configure a small executable wrapper if the editor
setting requires shell syntax.

After the editor exits, compose checks the exact staged fingerprint again. If
the index, HEAD, branch, relevant configuration, or repository policy changed,
it aborts and reports the private edited file path; it does not silently use
the stale text. It similarly leaves the file available when the edited message
violates body policy. On success it prints that path (or `output_path` in JSON)
and reports `committed: false`; no Git commit command is invoked. The user
chooses whether and how to use the file afterward.

The generated body wraps to the repository `commit.line_length` policy. User
edits are preserved byte-for-byte in the output file, but a successful result
also requires body-forbid/required policy compliance and wrapped body lines.
Only the generated staged-path facts receive a `proposed_body_grounding` result;
additional user-authored prose is deliberately preserved as user-authored, not
misrepresented as tool-grounded output.

`commit.body = "required"` can now use compose, while `forbid` rejects the
workflow. The proposal avoids repeating its subject sentence in the body.

For hintable ambiguity, typed prefixes now rerank only already prepared
matching candidates in their existing safe order. This happens in the existing
bounded helper; it does not generate, compile context, start a daemon, or ask a
question from the ZLE path.

Tests cover matching-prefix reranking, generated-body evidence/line wrapping,
editor preservation, owner-only files, forbidden shells, unchanged HEAD, and
staged-state changes during editing. They do not make a commit.
