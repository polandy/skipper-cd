---
name: pr-review
description: Thorough quality review of a pull request — docs/ADR sync, implementation quality, test coverage, UI docs + e2e tests, CI status (fix failures), and updating the branch (rebase or merge) if behind the target branch. Use when asked to review a PR by number or branch.
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
- Encapsulation: no raw mutable maps/structs passed around where a type with methods belongs; consumer-side interfaces (`Runner`, `CommitReader` style) instead of concrete coupling.
- Go conventions: sentinel errors + `errors.Is` (never string matching), `any` not `interface{}`, atomic writes (temp file + rename) for persisted state.
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
- Remember: manual-test-first — if Andy hasn't eyeballed the rendered UI yet, flag that as a gate before finalizing e2e tests, don't silently skip it.

## 6. CI status — fix failures

- Check `gh pr checks <PR>`. **All checks must be green.**
- If anything is red: investigate the failure log (`gh run view --log-failed`), fix it on the PR branch, run the local verify pipeline, commit with a Conventional Commit message (allowed types only: `feat`, `fix`, `docs`, `chore`, `refactor`, `test`, `ci` — never `build:`), push, and wait for CI to re-run. Repeat until green.

## 7. Branch freshness — update if behind

- Check whether the PR branch is behind the target branch: `git fetch origin && git rev-list --count <head>..origin/<base>`.
- If behind, bring it up to date. Either approach is fine — the PR is squash-merged into `main` anyway, so intermediate history doesn't matter:
  - **Rebase** onto the target branch (`git rebase origin/<base>`), then force-push with `--force-with-lease`, **or**
  - **Merge** the target branch in (`git merge origin/<base>`) and push normally — no force-push, and prior review comments stay anchored.

  Prefer the merge when a rebase would be painful (many commits, repeated conflicts) or when you want to avoid a force-push. Resolve any conflicts faithfully to both sides' intent.
- **After updating, re-run the entire checklist above** (sections 1–6): the update may have pulled in doc moves, new ADRs, or code the PR now conflicts with semantically even if git merged cleanly. Wait for CI to go green again.

## 8. Verdict

End with a concise report:

1. **Summary** — what the PR does, one paragraph.
2. **Findings** — per section above: ✅ ok / ⚠️ issue (with file:line) / 🔧 fixed by me (with commit).
3. **Blockers** — anything that must change before merge and that you could not fix yourself (e.g. missing manual UI check by Andy, design questions).
4. **Merge readiness** — ready / not ready. Do **not** merge; Andy merges via squash with a crafted Conventional Commit message on his own command.
