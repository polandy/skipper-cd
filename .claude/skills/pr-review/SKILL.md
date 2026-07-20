---
name: pr-review
description: Thorough quality review of a pull request — docs/ADR sync, implementation quality, test coverage, UI docs + e2e tests, CI status (fix failures), and merging the target branch in if the PR is behind. Posts the verdict as a PR comment when done. Use when asked to review a PR by number or branch.
argument-hint: <PR number or branch>
---

# PR Quality Review

You are a meticulous code reviewer for skipper-cd. Review the pull request given in `$ARGUMENTS` (a PR number like `97`, or a branch name; if omitted, use the PR for the current branch via `gh pr view`).

Work through **all** sections below in order. Collect findings as you go and fix what the instructions say to fix. Finish with a structured verdict.

## 0. Gather context

- `gh pr view <PR> --json title,body,baseRefName,headRefName,mergeStateStatus,statusCheckRollup` for metadata and CI status.
- `gh pr diff <PR>` for the full diff; check out the PR branch (or its worktree under `.claude/worktrees/` if one exists) so you can build and test.
- Read the PR description and any linked ADR first — the review checks the implementation *against its stated intent*.

## 1. Documentation ↔ implementation sync

- **Code-level docs**: doc comments on exported symbols match actual behavior; `CLAUDE.md` invariants still hold — if the PR changes behavior covered by an invariant, `CLAUDE.md` must be updated in the same PR.
- **User documentation** (`docs/` — published via MkDocs): every new/changed config field, stack option, API endpoint, event status, or metric must appear on the right page (`configuration.md`, `nixos.md`, `docker.md`, `metrics.md`, `state.md`, `autosync.md`). Check the reverse too: docs must not describe behavior the PR removed or changed. Do **not** run `mkdocs build` locally — CI verifies strict mode.
- **README.md** stays a slim landing page; only touch it if the feature list or quickstart is now wrong.

## 2. ADRs (`dev-docs/adr/`)

- Does the PR implement a design decision significant enough to need a **new ADR** that is missing?
- Does the PR contradict or supersede an **existing ADR**? If so, the ADR needs a status update (superseded/amended) or the PR needs to change.
- If the PR ships an ADR, verify the ADR's decision section matches what the code actually does.

## 3. Implementation quality

- Clean, readable, small files; names reveal behavior; no dead code left "for later".
- **Comment verbosity**: comments cover the essentials a developer new to the project needs — the non-obvious *what* plus the one *why*. Flag comments that over-explain, narrate the change, or restate what the code already says. When the code references an ADR, the comment can stay short: the rationale lives in the ADR, so don't duplicate it inline — a pointer (`// see ADR-00xx`) beats a paragraph. Also flag comments that become unnecessary once the value or logic is extracted into a well-named variable or method — prefer the self-documenting name over the comment.
- Encapsulation: no raw mutable maps/structs passed around where a type with methods belongs; consumer-side interfaces (`Runner`, `CommitReader` style) instead of concrete coupling.
- Go conventions: sentinel errors + `errors.Is` (never string matching), `any` not `interface{}`, atomic writes (temp file + rename) for persisted state.
- **Error handling**: errors are handled at the call site, not silently dropped; wrapped with context via `%w` when propagated (not string-matched); sentinel errors + `errors.Is` for classification. Handling is consistent in *style* but appropriate to context — flag copy-paste `return err` that loses context, or a genuinely fatal error that's only logged.
- Never touch or review `vendor/`; if `go.mod` changed, verify `go mod tidy && go mod vendor` was run and `vendor/` is in sync.
- Respect package boundaries (e.g. `internal/nixos` must stay free of docker/state/metrics/events knowledge).

## 4. Unit test coverage

- Every new behavior has table-style tests with behavior-revealing names (`TestDeployStack_SkipsWhenUnchanged` style); bug fixes include a test that would fail without the fix.
- Tests inject fake runners (`command.Runner`) and assert exact argv — no real docker/git except the two sanctioned exceptions (`internal/command`, `internal/git/integration_test.go`).
- Run `go test ./... -cover` and compare touched packages against `main` — flag coverage regressions.
- Run the full local verify: `go build ./... && go vet ./... && go test ./... && test -z "$(gofmt -l cmd internal)"`.

## 5. UI changes

If the PR touches `internal/ui/static/index.html` or UI behavior:

- **`internal/ui/UI_SPEC.md` must be updated** in the same PR to describe the new surface.
- User docs (`docs/`) must mention user-visible UI features where relevant.
- **e2e tests**: a new UI behavior needs a corresponding Playwright e2e test (the "Maske" units under the e2e suite — one unit per PR). Check that behavior assertions exist; visual snapshots only where the established baselines pattern applies. Do **not** run Playwright locally — push and let the `e2e-ui` CI job verify.
- **Mask-letter uniqueness**: the "Maske" letter (the `us-`/`ut-` file prefix, the `Uxn` test IDs, the `dev-docs/e2e-tests.md` §/heading) must be unique on the *target* branch. A PR that sat open can collide with a letter a concurrently-merged PR already claimed — `ls e2e/ui/tests/` on the updated branch and, if collided, rename yours to the next free letter across all three places (file, test IDs, spec section).
- Remember: manual-test-first — if the maintainer hasn't eyeballed the rendered UI yet, flag that as a gate before finalizing e2e tests, don't silently skip it.

## 6. CI status — fix failures

- Check `gh pr checks <PR>`. **All checks must be green.**
- If anything is red: investigate the failure log (`gh run view --log-failed`), fix it on the PR branch, run the local verify pipeline, commit with a Conventional Commit message (allowed types only: `feat`, `fix`, `docs`, `chore`, `refactor`, `test`, `ci` — never `build:`), push, and wait for CI to re-run. Repeat until green.

## 7. Branch freshness — update if behind

- Check whether the PR branch is behind the target branch: `git fetch origin && git rev-list --count <head>..origin/<base>`.
- If behind, **merge** the target branch in (`git merge origin/<base>`) and push normally. Always merge — never rebase: the PR is squash-merged into `main` anyway, so intermediate history doesn't matter, and merging avoids a force-push while keeping prior review comments anchored. Resolve any conflicts faithfully to both sides' intent.
- **After updating, re-run the entire checklist above** (sections 1–6): the update may have pulled in doc moves, new ADRs, or code the PR now conflicts with semantically even if git merged cleanly. Wait for CI to go green again.
- **Two things bite specifically after a merge when both PRs touch the UI** (git won't flag either — one is semantic, one is binary):
  - **Mask-letter collision** — the concurrently-merged PR may have taken your e2e "Maske" letter (§5). Rename yours to the next free letter (file + `Uxn` test IDs + `e2e-tests.md` section) so two masks don't share a letter.
  - **Shared visual snapshots** — both PRs likely regenerated the same baselines (`ua-deploys.spec.ts/deploys-table.png`, the `ud-chrome` full-page `theme-*`/`mobile-layout` ones), so those show up as **binary merge conflicts**. Never take one side: regenerate them **once with both changes present** (the pinned-container recipe, `reference-e2e-snapshot-regen`) and `git add` the result, so the baseline reflects the merged UI.

## 8. Verdict

End with a concise report:

1. **Summary** — what the PR does, one paragraph.
2. **Findings** — per section above: ✅ ok / ⚠️ issue (with file:line) / 🔧 fixed by me (with commit).
3. **Blockers** — anything that must change before merge and that you could not fix yourself (e.g. missing manual UI check by the maintainer, design questions).
4. **Merge readiness** — ready / not ready. Do **not** merge; the maintainer merges via squash with a crafted Conventional Commit message on their own command.

## 9. Post the verdict as a PR comment

Once the review is complete, post the verdict (the report from section 8) as a comment on the PR so the outcome is recorded there:

```
gh pr comment <PR> --body '<the verdict report, GitHub-flavored markdown>'
```

- Write the comment in **English** (docs/PRs are English-only), using the same Summary / Findings / Blockers / Merge readiness structure as the terminal report.
- Prefix it with a marker like `## 🤖 PR quality review` so it's clearly the automated review.
- If you fixed things and pushed commits, reference them so the comment reflects the final state, not the pre-fix state — post it **after** the last push and after CI has been re-triggered.
- If a prior automated-review comment from a previous run of this skill already exists on the PR, edit/replace that one instead of stacking duplicates (`gh pr comment --edit-last`, or delete the stale one), so the PR keeps a single up-to-date review comment.
