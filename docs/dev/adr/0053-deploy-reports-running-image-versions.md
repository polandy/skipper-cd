# ADR-0053: A deploy reports the versions it actually put into service

Status: accepted
Date: 2026-07-30

## Context

A terminal deploy event names the services whose version changed, as
`old → new` — the Deploys view's Version column and the line each notification
appends below its headline. That list is built by comparing the **image
references from the compose file** against the ones recorded for the previous
deploy (`state.yaml` → `images`).

That comparison cannot see the most common update in a homelab. A stack pinned
to a floating tag — `:latest`, `:stable`, `:30-apache` — keeps the same
reference string forever. The deploy is triggered by something else (an env
file, a watched directory, a compose edit), `pull` fetches a new image behind
the unchanged tag, and the version delta comes out **empty**: the notification
falls back to naming only the stack, on exactly the deploys where the version
question is most interesting.

The UI already had a second, more truthful source: the Stacks view reads what
each service *runs* via `docker compose ps`. But `ps` reports the image
*reference*, which is the same string that did not change — so it cannot answer
this either. The value that does move is the container's **image ID**, which
`docker compose images` reports.

Compose splits that answer across its two read commands, and neither half is
usable alone:

- `ps --format json` has `Service`, the container `Name` and the `Image`
  **reference** it runs — but no image id.
- `images --format json` has the image `ID` and a `ContainerName` — but **no
  `Service` field at all**, and its `Tag` is **empty** for every image pulled by
  digest (verified on Docker Compose 5.1.4).

So an identity built from `images` alone cannot be attributed to a service, and
building the reference from its `Repository`/`Tag` would *lose* the tag exactly
on digest-pinned images — turning a readable `v3.7.9 → v3.8.0` into two
unreadable hex ids. That matters: a host whose images are digest-pinned (a
Renovate-managed repo, where the digest *is* part of the reference) is the
common case, and the reference-based delta already works perfectly there.

The obvious fix — record the resolved image ID in `state.yaml` → `images` — is
wrong: that map is also what `hasAnyImageChanged` reads to decide whether to
`pull` at all (Invariant 5). Comparing a resolved ID against a desired
reference never matches, so every stack would pull on every run.

## Decision

**A successful deploy records what the stack's containers now run, and the next
deploy reports its version change against that.**

- After a successful apply, `ps` and `images` are read and **joined on the
  container name** — the only key both share — giving one identity per service.
- That identity is **the reference the container runs, plus the short image id
  when and only when the reference carries no digest**:
  - `traefik:v3.7.9@sha256:6529…` stays verbatim. It already identifies the
    image exactly, so a tag bump keeps reading as a tag bump.
  - `nextcloud:34-ghostscript` gains `@40c2d6f1d8f0`, because that reference
    alone is blind to a re-pull.

  The addition is therefore strictly additive: it never degrades a message that
  already worked, and adds the id exactly where the reference is blind. The id
  is truncated to docker's own 12-hex short form, so a docker that reports the
  full `sha256:` digest and one that reports the short ID normalize to the same
  value — a docker upgrade must not read as a rebuild.
- It is persisted under a **separate** top-level key, `running_images`. The
  existing `images` map keeps holding the compose file's *desired* references
  and remains the sole input to the pull decision. Two questions, two maps.
- `running_images` is **report-only**: it is not a hashed input, so it can never
  trigger a deploy, and it feeds nothing but the reported delta.

**The read is strictly additive.** It answers only when both sides are known —
a recorded baseline and a successful read now. Otherwise (no Outputter wired,
the command failed, its output did not parse, or the stack has no baseline yet)
the deploy keeps the compose-reference delta it has always reported and simply
records a baseline for next time. Nothing about the deploy's outcome depends on
it, and a failed read never clears an existing baseline — that would claim the
stack runs nothing.

**A deploy also reports whether it was gated.** The event carries
`health_gated`: true when an effective `deploy_health_check` was in force
(explicit, or inferred from a compose healthcheck — ADR-0046). On a success
that is the one thing the outcome alone does not say: applied, or verified
healthy. Notifications render it on success only; a failure's error text already
names what the gate did.

## Consequences

- A floating-tag redeploy now reports `app: 34-ghostscript@a1b2c3d4e5f6 →
  34-ghostscript@9f8e7d6c5b4a` instead of nothing, while a digest-pinned tag
  bump still reports `v3.7.9 → v3.8.0` exactly as before. The UI needs no
  change: it already renders a same-tag, moved-digest change as `tag ↻`
  (rebuilt) and a tag bump as the tags.
- Two extra compose reads per successful deploy (`ps` + `images`). They run
  through the same `Outputter` as rollout's `compose ps` read, which passes no
  env, so a compose file interpolating an `env_file` variable resolves it to
  empty here. That affects these reads only — the deploy itself runs with the
  full env — and a compose file that errors on the missing value degrades to the
  fallback above.
- A service `ps` does not account for is dropped rather than guessed at from its
  container name, and a failed `images` read discards the whole answer: half of
  it would make every floating-tag service look like it had just moved to a
  reference with no id.
- Services built from `build:` become version-tracked, which the
  compose-reference delta never could (they carry no `image:` to compare): `ps`
  reports the image name compose gave them, so a rebuild that produced a new
  image reads as an image-id change under the unchanged name. That is a real,
  reportable version change, so it is kept, and the docs say so.
- A service in the baseline that `ps` no longer lists is reported as removed
  only when it is also gone from the compose file. A declared service without a
  container — an inactive profile, a scale of zero — is not a removal, and
  claiming `<old> (removed)` for it would be false. When the compose parse is
  unavailable the raw delta stands: suppressing removals blindly would hide the
  real ones.
- The first deploy of each stack after upgrading reports the old
  compose-reference delta (usually empty) and establishes the baseline. From the
  second one on, the version delta is the real one.
- `state.yaml` grows by one line per service per stack.
