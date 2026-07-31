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
`docker compose images` reports per service.

The obvious fix — record the resolved image ID in `state.yaml` → `images` — is
wrong: that map is also what `hasAnyImageChanged` reads to decide whether to
`pull` at all (Invariant 5). Comparing a resolved ID against a desired
reference never matches, so every stack would pull on every run.

## Decision

**A successful deploy records what the stack's containers now run, and the next
deploy reports its version change against that.**

- After a successful apply, `docker compose images --format json` gives one
  identity per service: `<repository>:<tag>@<short image id>`. The ID is
  truncated to docker's own 12-hex short form, so a docker that reports the full
  `sha256:` digest and one that reports the short ID normalize to the same value
  — a docker upgrade must not read as a rebuild.
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

- A `:latest` redeploy now reports `app: 30-apache@a1b2c3d4e5f6 →
  30-apache@9f8e7d6c5b4a` instead of nothing. The UI needs no change: it already
  renders a same-tag, moved-digest change as `tag ↻` (rebuilt).
- One extra `docker compose images` per successful deploy. It runs through the
  same `Outputter` as rollout's `compose ps` read, which passes no env, so a
  compose file interpolating an `env_file` variable resolves it to empty here.
  That affects this read only — the deploy itself runs with the full env — and a
  compose file that errors on the missing value degrades to the fallback above.
- The first deploy of each stack after upgrading reports the old
  compose-reference delta (usually empty) and establishes the baseline. From the
  second one on, the version delta is the real one.
- `state.yaml` grows by one line per service per stack.
