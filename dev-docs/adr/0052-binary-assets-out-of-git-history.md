# ADR-0052: Keep churning binary assets out of git history

- **Status**: accepted
- **Date**: 2026-07-26
- **Supersedes / amends**: nothing (new policy; complements ADR-0035, which
  requires the UI's fonts and icons to be *embedded* and therefore committed)

## Context

Two kinds of binary live in this repo, and they behave very differently over time.

**Write-once binaries** — the logo (2 × 12 KB), the embedded web fonts
(6 × ~17 KB, required by ADR-0035's self-contained UI), the rasterised PWA icons
(4, 20 KB). They were added once and essentially never change: they cost their
size a single time.

**Churning binaries** — PNG screenshots. A PNG never delta-compresses, so every
regeneration stores its full size again, forever:

| path | blobs in history | bytes |
| --- | --- | --- |
| `e2e/ui/__screenshots__` (Playwright baselines) | 62 | 2.6 MB |
| `docs/assets/screenshots` (landing page) | 4 | 1.2 MB |
| `vendor/` (text, committed for the nix flake) | 930 | 2.0 MB |

Both screenshot sets regenerate on ordinary UI work — a header or deploy-table
change rewrites up to six baselines at once (four in a single PR, #210), and a
Stacks-view change rewrites the docs stills. Left alone, the two paths were on
track to outgrow every other binary in the repo, in a repo whose whole pack is
~18 MB.

Removing the *existing* blobs is a separate question from stopping the growth,
and much more expensive: a history rewrite orphans the commit SHA the homelab
flake pins, kills every permalink in ADRs, PRs and issues, forces everyone to
re-clone — and on GitHub the old blobs stay reachable through `refs/pull/*`
anyway, so the hosted repository barely shrinks. The prize for the docs path was
~0.5 MB.

## Decision

**Churning binaries do not enter git history. Write-once binaries stay committed.**
Existing blobs are left in history — this is a forward-only policy, not a
rewrite. Per asset class:

1. **Docs landing-page screenshots** — *generated, not stored*. They are derived
   from the UI in the same commit, so the docs workflow renders them from a
   seeded instance before `mkdocs build --strict`, and `make docs-screenshots`
   does the same locally. `docs/assets/screenshots/*.png` is gitignored.
2. **Playwright visual baselines** — *tracked with Git LFS*. A baseline is the
   test oracle, so it cannot be generated; but it also has to stay reviewable in
   a PR diff, which rules out moving it to release assets or an external
   visual-regression service. LFS keeps a text pointer in history and the bytes
   in LFS storage, and GitHub renders LFS images in diffs normally.
3. **Write-once binaries** — unchanged, committed as normal files.

Because Playwright's pinned container ships no `git-lfs`, the baselines are
fetched in a small `baselines` CI job on a runner that has it and handed to the
`e2e-ui` job as an artifact, which overwrites the pointer files the container's
own checkout wrote. The container itself stays untouched: it is pinned precisely
so font rasterisation matches how the baselines were generated, and installing
packages into it at run time would undermine that.

## Alternatives considered

- **Optimise, don't move** (`oxipng -o max`, lossless and pixel-identical, so the
  compares still pass: 70 KB → 42 KB). Rejected as the *primary* answer — it
  slows the growth by 40 % but does not stop it. Still worth applying to any PNG
  that must be committed.
- **Lossy recompression** (pngquant / WebP, 325 KB → 50 KB). Fine for the docs
  images, fatal for baselines: Playwright compares pixels exactly.
- **History rewrite** (`git filter-repo` / `git lfs migrate`) to reclaim the
  existing blobs. Rejected: see the flake-pin, permalink, re-clone and
  `refs/pull/*` costs above, for ~0.5 MB.
- **Baselines as release assets or object storage.** Repo stays small, but a
  baseline stops being visible in the PR diff — the property that makes it
  reviewable at all.
- **A visual-regression service** (Argos, Percy, Chromatic, Lost Pixel). The
  industry answer for this half, with OSS free tiers. Rejected for now: it puts
  the review gate behind an external service and a token, against the
  self-contained ethos this project holds elsewhere.
- **Actions cache as the baseline store.** Unusable — caches are evicted after
  ~7 days idle, which would surface as "missing baseline" flakes.
- **Fewer or narrower baselines** (element-scoped instead of the three full-page
  `ud-chrome` shots, which nearly every header change regenerates). This would
  cut the churn at its source, but it trades away coverage — a full-page shot
  catches drift nobody asserted. Kept as an option for the day the baseline set
  grows, not taken as a size measure.

## Consequences

- The repository stops accumulating screenshot bytes: ~700 KB per docs
  regeneration and ~175 KB per baseline regeneration no longer land in the pack.
- **Working on the UI suite now requires `git-lfs`.** `nix develop` provides it;
  otherwise `git lfs install && git lfs pull` once per clone. Without it the
  baselines check out as ~130-byte pointer text and the pixel compares fail — a
  confusing failure mode, so it is called out in `.gitattributes`,
  `dev-docs/e2e-tests.md` §5 and `CLAUDE.md`.
- A local `mkdocs build --strict` needs one `make docs-screenshots` run first,
  since the images it embeds are no longer in the tree.
- CI grows two small jobs (`screenshots` in the docs workflow, `baselines` in
  the CI workflow). Both are cheap (~50 s and ~15 s) and both fail loudly.
- GitHub LFS quota (1 GB storage / 1 GB bandwidth per month on the free tier) is
  now a resource this repo consumes: at ~300 KB per baseline set, that is years
  of headroom, but it is a new limit to be aware of.
- The policy is only worth its machinery while the *number* of churning binaries
  stays small. If baselines multiply, revisit the narrower-baseline option above
  before adding more infrastructure.
