# Local preference learning

Learning is enabled by default and is local to the repository identity already
used for cache isolation. Linked worktrees share one profile; staged candidate
caches remain worktree-specific. It reranks only candidates that have already
passed repository policy and grounding. It never fine-tunes a model, creates a
suggestion frontend, authorizes a rejected candidate, or changes a commit.

Profiles live below `$XDG_DATA_HOME/zsh-git-inlay/learning` (or the configured
data-directory override), in private `0700` storage with one `0600`, versioned
file per hashed repository identity. There are at most 128 profiles and each
is capped at 16 KiB. A profile contains only bounded aggregate counts:
Conventional Commit type/scope, subject length, lowercase/imperative and
subject-only tendencies, and a small allowlist of preferred or weakly avoided
generic verbs. It contains no commit subject or body, source, diff, activity,
provider state, remote URL, credential, or candidate text.

```zsh
zsh-git-inlay learning status --cwd .
zsh-git-inlay learning inspect --cwd . --json
zsh-git-inlay learning disable --cwd .
zsh-git-inlay learning enable --cwd .
zsh-git-inlay learning reset --cwd .
zsh-git-inlay learning export --cwd . > profile.json
zsh-git-inlay learning import --cwd . --file profile.json
```

`reset` removes the current repository profile completely. `disable` takes
effect immediately for new observations and invalidates this repository's
prepared candidates in a running daemon. An export contains only aggregate
style data; importing it is explicit and writes it under the current identity.

## Evidence and ranking

The Zsh hook does not parse or retain `git commit` arguments. For a recognized
commit it asks the daemon asynchronously to keep a short-lived in-memory
pre-commit candidate snapshot. After a successful command, the daemon requires
an actual HEAD change, reads the final local Git subject/body once, reduces it
to aggregates, then discards the strings. Aborted commits and commits without a
HEAD transition create no observation.

An exact final-subject match is classified as the primary or an alternate
prepared candidate. A non-exact subject with the same Conventional Commit type
and scope as the first prepared candidate is a weak `edited_candidate`
classification; this is an inference, not proof that the suggestion was
accepted and edited. Other final subjects are user-authored. Candidate display
or cycling alone never updates a profile. A weak edited signal can only add one
allowlisted avoided-verb count.

During background generation, the daemon separately derives a bounded
historical repository prior from the latest 64 local commit metadata records.
It retains only the same aggregates and does not write that prior to the
profile. `learning inspect` shows local interactions and this prior separately.

Grounding and repository policy run first. Learning then applies a stable,
bounded reranking adjustment (local at most +6/-6 and historical at most
+3/-3); it cannot introduce an invalid type/scope, bypass a required body,
revive an ungrounded candidate, or out-rank policy validation.
`zsh-git-inlay explain --json` shows applied aggregate reasons and scores. At
4096 observations, statistics decay rather than grow indefinitely.

## Explicit clone sharing

The tool never searches the filesystem for clones and never automatically
reuses a profile. To copy aggregate style data from a known local clone, supply
that clone and confirm after the tool compares normalized `origin` hashes.
Userinfo, query parameters, and fragments are removed before comparison and no
remote URL is retained.

```zsh
zsh-git-inlay learning clone-import --cwd . --from /path/to/clone
# reports that explicit confirmation is required
zsh-git-inlay learning clone-import --cwd . --from /path/to/clone --confirm
```

Only the exportable aggregate profile is copied. Activity, source summaries,
candidates, provider grants, credentials, and cache data are excluded.
