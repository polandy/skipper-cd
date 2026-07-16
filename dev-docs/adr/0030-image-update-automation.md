# ADR-0030: Image-update automation

Status: accepted
Date: 2026-07-16

## Context

skipper-cd redeploys a stack when a *tracked file* changes in git: the compose
file, `env_files`, `vars_file`, `watch_dirs`, and `build:` Dockerfiles (Invariant
2). For the `image:` references specifically, change detection compares the
**reference string** parsed from the compose file against the last deployed one
(`hasAnyImageChanged`, Invariant 5) — and `compose pull` only runs when that string
moved. So a stack pinned as `image: caddy:2.8.4` redeploys the moment git changes
the tag to `2.8.5`, and `image: postgres:16-alpine@sha256:abc…` the moment the
digest changes.

The gap is **mutable tags**. `image: caddy:latest` (or `:2`, `:stable`, `:16`) in
git never changes its *text* when the registry publishes a new image behind the
same tag. skipper sees an unchanged compose file, an unchanged image string, and
does nothing — the running container keeps the old digest indefinitely. Crucially,
**neither the periodic reconcile loop ([ADR-0028](0028-periodic-reconcile-loop.md))
nor the health poller ([ADR-0027](0027-live-stack-health-in-ui.md)) closes this**:
reconcile compares git desired state (unchanged text → no-op), and the poller only
*reports* runtime health. This is the last big "ArgoCD for docker-compose" gap —
the one that turns skipper from a *compose redeployer* into a tool that acts on new
upstream releases.

The tension is that acting on a new registry digest means reacting to a signal
**that does not come from git** — the first non-git deploy trigger skipper would
ever have. skipper's whole identity is "git is the desired state; the running state
equals git" ([[project-scope-visualization-not-trigger]]). Any design here is
really a decision about *how far* to bend that, so the three shapes below differ
mostly in what they do to the git-single-source-of-truth invariant.

### The three shapes

- **(A) Write-back to git (GitOps-conformant).** A watcher resolves the current
  registry digest for each tracked mutable tag; when it moves, skipper **commits
  the updated, digest-pinned reference back to the deploy repo** (e.g.
  `image: caddy:latest` → `image: caddy:latest@sha256:new…`) and pushes. That push
  is an ordinary change — the existing webhook/reconcile path detects it, pulls,
  and deploys. Git stays the single source of truth; it just gains an automated
  author, exactly like ArgoCD Image Updater's write-back mode or Renovate. **The
  deploy is still git-driven** — the registry only drives a *commit*, not a deploy.
- **(B) Direct pull + redeploy (Watchtower-style).** On a moved digest, skipper
  runs `compose pull && up -d` **without touching git**. Simplest to build, but it
  makes the running state diverge from git on purpose: git says `latest`, the host
  runs a digest git never recorded. That breaks the invariant skipper is built on
  and leaves no audit trail in the repo — a hard no in a viz/GitOps tool.
- **(C) Delegate to Renovate / dependabot on the deploy repo, in-repo non-goal.**
  Renovate already watches `image:` refs in docker-compose files, bumps tags **and
  pins digests**, and opens/merges a PR — whose merge fires the webhook skipper
  already consumes. skipper writes **zero** new code and gets digest pinning,
  changelogs, scheduling, and grouping for free. The cost is an external service
  and its config living outside skipper.

## Decision

**Primary recommendation: adopt (C) — delegate to Renovate — and document it as the
supported path, treating an in-repo image watcher as a deliberate non-goal for
now.** If an in-repo watcher is ever built, it **must be shape (A) (write-back to
git); shape (B) is rejected outright** as breaking the core invariant.

The reasoning is the same discipline that kept [ADR-0028](0028-periodic-reconcile-loop.md)
in scope: skipper deploys *what a push would deploy*. Renovate makes "a new upstream
image" arrive **as a push**, so skipper needs no new trigger surface, no registry
client, no registry credentials, and no new failure modes — the entire feature
reduces to a webhook it already handles, plus the reconcile loop as the safety net
if Renovate's merge webhook is ever lost. It is also strictly more capable than a
first-cut watcher (semver ranges, digest pinning, release notes, grouped updates,
maintenance windows) and keeps the deploy repo as the complete, auditable history
of every image change — which is precisely what a GitOps viz tool should show.

This ADR therefore proposes to **ship documentation, not code**: a
`docs/configuration.md` / README section ("Keeping images up to date") with a
minimal `renovate.json` for the deploy repo (docker-compose manager, digest
pinning, auto-merge policy) and a note that the merge flows through the normal
deploy path. No change to `internal/deploy`, config, or state.

### If an in-repo watcher is later justified — shape (A) sketch

Should delegation prove insufficient (e.g. an air-gapped forge with no Renovate, or
a desire to keep skipper self-contained), the follow-up ADR should build a
**write-back watcher**, opt-in per stack/service, reusing existing machinery rather
than inventing a parallel deploy path:

- **Opt-in only, mutable tags only.** A per-stack `image_updates:` policy naming
  which services to track and the strategy — `digest` (repin the same tag's newest
  digest) or `semver`/`tag` (adopt a newer matching tag). Absent = today's
  behaviour. Fully-pinned digests and immutable version tags are never touched;
  they already redeploy via git.
- **Registry reads through the `Runner` seam.** Resolve digests by shelling out to
  an existing tool (`docker manifest inspect` / `skopeo` / `crane`) via the
  `command.Runner` abstraction ([ADR-0003](0003-runner-abstraction-and-fake-based-tests.md)),
  so the whole thing stays fake-injectable in tests and never adds an HTTP registry
  client or auth code path of its own (it reuses the host's docker/registry creds).
- **The action is a git commit, not a deploy.** The watcher's only side effect is
  `git commit`+`push` of the repinned reference to the deploy repo. Deployment then
  happens through the unchanged webhook/reconcile → hash → pull → up path. This
  keeps every downstream invariant (autosync gate, queue, rollback, health gate)
  intact for free, exactly as ADR-0028's reconcile does.
- **Its own timer, UI-independent.** Like reconcile (and unlike the UI-gated
  pollers), a correctness/automation loop must run headless; gated only by its own
  `image_update_interval_seconds` (default off / `0`, opt-in), with a per-host NixOS
  module option in the style of `healthPollIntervalSeconds`.

Whether to build (A) at all is the real open question; this ADR's position is *not
yet* — delegation covers the homelab today.

## Consequences

- **With (C):** the "mutable tag never updates" gap is closed with no new skipper
  surface. Image updates arrive as commits in the deploy repo — visible in
  skipper's own diff/commit view ([[project-todo-diff-commit-metadata]]) like any
  other change — and deploy through the path skipper already exercises and tests.
  The dependency is operational (run Renovate against the deploy repo), not code.
- **Scope stays intact.** No non-git deploy trigger is introduced; git remains the
  single source of truth and the audit log. skipper keeps doing exactly what a push
  does — the property that made ADR-0028 obviously in-scope.
- **Digest pinning is the enabling detail** either way: an update is only
  observable to skipper once it lands in git as a changed reference string, which
  `hasAnyImageChanged` already detects. Both (A) and (C) work by *making the tag's
  new digest appear in git*; they differ only in who writes it.
- **Deferring (A) costs little.** If it is built later, it slots in as a commit
  producer ahead of the existing pipeline — no rework of change detection, pull
  logic, or rollback. The interface (`Runner`-based digest reads, opt-in policy) is
  additive.
- **Explicit non-goal recorded:** shape (B) (Watchtower-style direct pull without
  git) is rejected, so future contributors don't reintroduce runtime/git divergence
  under the "image automation" banner.

## Alternatives considered

- **(B) Watchtower-style direct pull + `up -d`, no git.** Rejected: it deliberately
  diverges the running state from git and leaves no audit trail in the repo — the
  opposite of skipper's GitOps/viz model. The convenience is real but buys a
  permanent invariant violation.
- **Build shape (A) now instead of delegating.** Rejected *for now*, not on
  principle: it adds a registry-reading component, an opt-in policy schema, a
  git-writer with push credentials, and a new timer — real surface — to do what
  Renovate already does better and more safely. Revisit only if a concrete
  deployment can't run Renovate against its forge, at which point (A) is the
  sanctioned shape.
- **Store the resolved digest in `state.yaml` and diff against the registry each
  reconcile (no git write-back).** A middle path: keep watching in skipper but act
  by pulling, comparing the deployed digest to the registry's current one. Rejected
  for the same reason as (B) — the decision to move to a new digest would live only
  in skipper's state, not in git, so git would no longer describe what runs.
- **Do nothing / status quo.** Honest option: pinned-tag users already get updates
  via git, and mutable-tag users can pin manually. Rejected as the *documented*
  answer because "how do I auto-update images?" is a first-class CD question and
  deserves a supported answer (C) rather than silence.

## Appendix: draft docs section (not yet published)

This is the concrete deliverable of decision (C) — the "ship documentation, not
code" section. It is kept here as a draft and only lands in the published
`docs/configuration.md` (and a short README pointer) once this ADR is **accepted**.

The design assumption baked into it is the one skipper's own change detection needs:
**Renovate pins every image to a digest** (`docker:pinDigests`). Once every `image:`
reference in the deploy repo carries a `@sha256:…`, *every* upstream change — a new
patch release **and** a mutable tag whose digest silently moved — becomes a changed
reference string in git, which `hasAnyImageChanged` (Invariant 5) already detects and
pulls. The "mutable tag never updates" blind spot disappears entirely, with no
skipper-side code.

---

### Keeping images up to date

skipper deploys the images your compose files declare; it does **not** watch
registries for new versions on its own (a deliberate scope choice — see
[ADR-0030](../dev-docs/adr/0030-image-update-automation.md)). Instead, let
[Renovate](https://docs.renovatebot.com/) keep the `image:` references in your
**deploy repo** current. Renovate opens (or auto-merges) a change that bumps the
reference and **pins it to a digest**; that merge is an ordinary push, so skipper's
existing webhook + [periodic reconcile](configuration.md#periodic-reconcile) path
picks it up and redeploys — no skipper configuration required.

Why digest pinning matters: with a plain mutable tag like `image: caddy:latest`, the
text in git never changes when the registry publishes a new image, so skipper never
sees a change. Renovate's digest pinning turns the reference into
`image: caddy:2.8.4@sha256:…` and keeps that digest current — so **every** update
lands in git as a changed reference and deploys through the normal path.

Minimal `renovate.json` for the deploy repo:

```json
{
  "$schema": "https://docs.renovatebot.com/renovate-schema.json",
  "extends": ["config:recommended", "docker:pinDigests"],
  "packageRules": [
    {
      "matchManagers": ["docker-compose"],
      "automerge": true,
      "automergeType": "pr"
    }
  ]
}
```

- `docker:pinDigests` makes Renovate pin (and keep current) a `@sha256:…` digest on
  every image reference — the key that closes the mutable-tag gap.
- The `docker-compose` manager covers `docker-compose.yml` / `compose.yaml` `image:`
  fields out of the box.
- `automerge: true` lets low-risk updates merge without review, so the deploy is
  hands-off end to end. Drop it to keep every bump behind a reviewed PR — the merge
  still triggers the deploy either way.

On a Gitea/Gogs forge (the homelab setup), run **self-hosted** Renovate — the hosted
Mend app is GitHub-only. For example, a scheduled `renovatebot/renovate` container
with `RENOVATE_PLATFORM=gitea`, a bot token, and `RENOVATE_AUTODISCOVER=true` (or an
explicit repo list) pointed at the deploy repo. A missed merge webhook is not fatal:
the reconcile loop re-fetches the branch tip within its interval and converges.

> Prefer a self-contained skipper with no external updater? That would be an in-repo
> registry watcher, considered and deferred in
> [ADR-0030](../dev-docs/adr/0030-image-update-automation.md); Renovate is the
> supported path today.

---

*(The cross-links above use `docs/`-relative paths as they would read once moved into
`docs/configuration.md`; the ADR link would become an absolute GitHub URL there, per
the repo rule that published pages never `../` into `dev-docs/`.)*
