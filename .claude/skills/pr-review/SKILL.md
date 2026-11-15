---
name: pr-review
description: Thorough quality review of a pull request — docs/ADR sync, conformance to the project coding guidelines (CLAUDE.md invariants, engineering principles, Go conventions), implementation quality, test coverage, UI docs + e2e tests, CI status (fix failures), and merging the target branch in if the PR is behind. Posts the verdict as a PR comment when done. Use when asked to review a PR by number or branch.
argument-hint: <PR number or branch>
---

# PR Quality Review

You are a meticulous code reviewer for skipper-cd. Review the pull request given in `$ARGUMENTS` (a PR number like `97`, or a branch name; if omitted, use the PR for the current branch via `gh pr view`).

Work through **all** sections below in order. Collect findings as you go and fix what the instructions say to fix. Finish with a structured verdict.

## 0. Gather context

- `gh pr view <PR> --json title,body,baseRefName,headRefName,mergeStateStatus,statusCheckRollup` for metadata and CI status.
- `gh pr diff <PR>` for the full diff; check out the PR branch (or its worktree under `.claude/worktrees/` if one exists) so you can build and test.
- Read the PR description and any linked ADR first — the review checks the implementation *against its stated intent*.
- **Load the project coding guidelines as the review baseline**: read the repo's `CLAUDE.md` — its `## Invariants`, `## Testing`, `## Engineering principles`, and `## Don'ts & pointers` sections are the authoritative standard this review enforces (plus the reviewer's global principles). Section 3 below distills the highest-signal checks, but treat `CLAUDE.md` as the source of truth: if it changed in this PR or since this skill was written, the *file* wins, not this skill's summary. Also skim `.golangci.yml` for the enabled linters.

## 1. Documentation ↔ implementation sync

- **Code-level docs**: doc comments on exported symbols match actual behavior; `CLAUDE.md` invariants still hold — if the PR changes behavior covered by an invariant, `CLAUDE.md` must be updated in the same PR.
- **User documentation** (`docs/user/` — published via MkDocs): every new/changed config field, stack option, API endpoint, event status, or metric must appear on the right page (`configuration.md`, `nixos.md`, `docker.md`, `metrics.md`, `state.md`, `autosync.md`). Check the reverse too: docs must not describe behavior the PR removed or changed. Do **not** run `mkdocs build` locally — CI verifies strict mode.
- **README.md** stays a slim landing page; only touch it if the feature list or quickstart is now wrong.

## 2. ADRs (`docs/dev/adr/`)

- Does the PR implement a design decision significant enough to need a **new ADR** that is missing?
- Does the PR contradict or supersede an **existing ADR**? If so, the ADR needs a status update (superseded/amended) or the PR needs to change.
- If the PR ships an ADR, verify the ADR's decision section matches what the code actually does.

## 3. Implementation quality — against the project coding guidelines

`CLAUDE.md` §Engineering principles / §Invariants / §Packages / §Don'ts (loaded in §0) is the standard — don't re-derive it here, apply it to the diff. Flag freshly-introduced violations of the principles it lists: magic strings/numbers not hoisted to a constant, a discarded error that matters (`_ = fn()` on a state-persisting/safety-critical call), string-matched errors instead of `errors.Is`, `interface{}` for `any`, non-atomic writes of persisted state, an exported symbol without a doc comment, raw mutable maps/structs where a type-with-methods belongs, or a broken package boundary (`internal/nixos` free of docker/state/metrics knowledge; one shared compose parser; UI self-contained per ADR-0035). The review-specific judgment calls that go beyond a flat rule:

- Clean, readable, small files; names reveal behavior; no dead code left "for later" (delete it — unused APIs are complexity, not future-proofing).
- **Comment verbosity**: comments cover the essentials a developer new to the project needs — the non-obvious *what* plus the one *why*. Flag comments that over-explain, narrate the change, or restate what the code already says. When the code references an ADR, the comment can stay short: the rationale lives in the ADR, so don't duplicate it inline — a pointer (`// see ADR-00xx`) beats a paragraph. Also flag comments that become unnecessary once the value or logic is extracted into a well-named variable or method — prefer the self-documenting name over the comment.
- **Error handling** in context: consistent in *style* but appropriate to the call site — flag copy-paste `return err` that loses context (should wrap with `%w`), or a genuinely fatal error that's only logged.
- **Invariant walk**: if the PR touches `internal/deploy`, `internal/config`, `internal/nixos`, or state persistence, walk the numbered `CLAUDE.md` §Invariants and confirm none is broken. A silently-broken invariant is a blocker even when tests pass.
- If `go.mod` changed, verify `go mod tidy && go mod vendor` was run and `vendor/` is in sync (never review `vendor/` itself).

## 4. Unit test coverage

Enforce `CLAUDE.md` §Testing + §Engineering-principles-test-first against the diff; the review-specific checks:

- **Test-first / behavior coverage**: every new behavior has a table-style test with a behavior-revealing name; each bug fix has a test that would fail without it; a safety/correctness invariant has its **failure paths** covered, not just the happy path. Missing any of these is a finding.
- **No non-deterministic timing** (§Testing + the reviewer's global principle): flag any test that leans on sleeps, fixed waits for async work, or polling for an effect that only *probably* lands — the fix is a deterministic seam in the production code, not a longer wait.
- Run `go test ./... -cover` and compare touched packages against `main` — flag coverage regressions.
- Run the full local verify exactly as `CLAUDE.md` §Commands defines it: `go build ./... && go vet ./... && go test ./... && test -z "$(gofmt -l cmd internal)"`, plus `golangci-lint run ./...` (locally `nix run nixpkgs#golangci-lint -- run ./...`) and `go test -race ./...` — CI runs both, so a red lint or race is a fix-it, not an FYI (§6).

## 4b. e2e ↔ requirements/spec sync — a gate before every merge

Every spec section or criterion the diff adds or changes (`internal/ui/UI_SPEC.md`, `docs/dev/*-spec.md`, an ADR outcome, a `CLAUDE.md` invariant) gets a row in the review table naming its e2e case (file + case ID), or one line saying why the unit layer is right. A flow across client and server, or a file/system change (webhook → deploy → event, rollback, hold, notification, host check), needs e2e; NixOS internals are unit by design.

- **Backend behavior the user sees** (`internal/deploy`, `webhook`, `config`, a new event status, gate, hold or notification) needs a pipeline case (`P<n>` in `e2e/`, listed in `docs/dev/e2e-tests.md` §4.1) or the one-line unit justification. Neither is a finding-free pass.
- **Reverse direction**: grep `e2e/` and `e2e-tests.md` for cases still asserting the changed or removed behavior (event statuses, labels, config keys, endpoints) and update them in the same PR. A stale assertion is a finding.
- **A new e2e test without a spec reference** (mask, ADR, §section in the header; an entry in `e2e-tests.md`) is a finding.
- **Spec sentence vs assertions**: read the touched spec sentences against the test body; every promise is an assertion, or the sentence changes. An absence claim needs its positive signal (`CLAUDE.md` §Testing).
- **Verdict**: a PR with e2e still owed is **not ready**. If it is merged anyway, list every debt in the verdict as `e2e owed: <case>` and add each to `TODO.md` in the same PR.

## 5. UI changes

If the PR touches `internal/ui/static/index.html` or UI behavior:

- **`internal/ui/UI_SPEC.md` must be updated** in the same PR to describe the new surface.
- User docs (`docs/user/`) must mention user-visible UI features where relevant.
- **e2e tests ↔ spec, in sync before merge**: a new or changed UI behavior has its Playwright case, and `docs/dev/e2e-tests.md` names it in **both** its §4 mask section (Given/When/Then) and the §6 traceability table — the same `Uxn` ID in the file name prefix, the `describe`/`test` title and the doc. A case in the suite but not in the doc, a doc case with no test, or a reused ID is a finding. Behavior assertions, not CSS reading; visual snapshots only where the established baselines pattern applies. Playwright runs in the `e2e-ui` CI job; run `make e2e-ui` locally only on a host with the headroom for it (`CLAUDE.md` §Workflow).
- **Headline case is mutation-proven**: revert the fix (or the guard) in production code, see that exact case go red, restore. A case that stays green is a finding.
- **Mask-letter uniqueness**: the "Maske" letter (the `us-`/`ut-` file prefix, the `Uxn` test IDs, the `docs/dev/e2e-tests.md` §/heading) must be unique on the *target* branch. A PR that sat open can collide with a letter a concurrently-merged PR already claimed — `ls e2e/ui/tests/` on the updated branch and, if collided, rename yours to the next free letter across all three places (file, test IDs, spec section).
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
