# ADR-0057: The build context is pinned to the repo clone

Status: accepted
Date: 2026-08-27

## Context

Invariant 1 says change detection and the compose file always come from the repo
clone. Invariant 2 lists the Dockerfiles of `build:` services among the hashed
inputs — resolved, like everything else, against the stack's directory in the
clone.

`project_directory` (ADR-0045) points somewhere else on purpose. It exists so a
stack's compose project keeps its identity and its `.env`, and so relative bind
mounts resolve to the tree that holds the runtime data — typically a NixOS
modules directory, not the clone.

The two only look independent. Docker Compose resolves **every** relative path in
the compose file against the project directory, and a `build:` context is a
relative path like any other. So `--project-directory` silently moves the build
inputs too:

```yaml
services:
  app:
    build:
      context: .          # resolved under --project-directory, not next to this file
      dockerfile: Dockerfile
    image: nextcloud:34-ghostscript
```

Skipper hashes `<clone>/modules/nextcloud/Dockerfile`; compose builds from
`<project_directory>/Dockerfile`. Two files, one name, and nothing connects them.

Observed on a live host. A dependency bot bumped a Dockerfile's base image from
`nextcloud:34.0.2` to `34.0.3` and pushed. Skipper saw the hashed Dockerfile
change and deployed. The build read the *other* copy — still on 34.0.2 —
found every layer cached, re-tagged the identical image, and `up` had nothing to
recreate. The run finished in 2.9 s, recorded a success, and reported no version
change, because there genuinely was none. The stack stayed on the old version
with a green history behind it.

The reported-version machinery (ADR-0053) behaved correctly throughout: it
compares what the containers actually run, and they ran what they had run
before. The silence was accurate. The deploy was not.

## Decision

**The build reads the tree skipper hashed.** Before `docker compose build`,
skipper generates a compose override that rewrites every *relative* build
context to its absolute path under the stack's directory in the clone, and
layers it on with a second `-f`:

```yaml
services:
  app:
    build:
      context: /var/lib/skipper/repo/modules/nextcloud
```

The override is deliberately minimal. It carries `build.context` and nothing
else, so compose merges it into the stack's own build section and leaves
`dockerfile`, `args` and `target` exactly as written — a `dockerfile:` stays
relative to the context and therefore follows it into the clone.

`--project-directory` is still passed on the same invocation. Project name,
`.env` loading and every relative bind mount keep resolving against the project
directory, unchanged and untouched: the override moves the build inputs and
nothing else.

It is generated for the **build call only**. Every other compose invocation —
`pull`, `up`, the rollout's `ps` reads, rollback — runs exactly as before.

Two cases produce no override at all, because there is nothing to pin:

- **No `project_directory`.** Compose already resolves the context against the
  compose file's own directory, which is the clone.
- **An absolute context.** It names the tree it builds from; the operator said
  where, and skipper does not second-guess it.

## Consequences

- **A change skipper deploys on is a change the build sees.** The hashed
  Dockerfile and the built Dockerfile are the same file, so a base-image bump
  reaches the running container and shows up as a version change.
- **A stale project directory stops mattering for builds.** The clone is
  synced every run by definition; the project directory is whatever the host
  put there. Only the runtime data it holds is still read from it.
- **A stack whose build context lives *only* in the project directory breaks.**
  Generated artifacts sitting outside the repo are no longer picked up. That
  arrangement was already broken in the direction that matters — skipper hashes
  the clone, so changes to those artifacts never triggered a deploy in the first
  place — and it now fails loudly at build time instead of deploying something
  nobody asked for. An absolute `context:` is the way to keep it.
- **One more temp file per build.** Written to the system temp dir and removed
  when the stack's apply finishes, the same shape rollback already uses for the
  restored compose file (Invariant 3).
- **Nothing to configure.** No key, no opt-out. Building from a tree other than
  the one that decided to build is not a mode worth offering.

## Alternatives considered

- **Fail the stack when the two Dockerfiles differ.** Skipper knows both paths
  and could compare them before building, turning the silent no-op into a loud,
  actionable error. Rejected as the primary fix: it reports the problem
  correctly but does not deploy the change, so every base-image bump would wait
  for a human to sync a second tree by hand. Making the deploy right is worth
  more than describing why it was wrong.
- **Resolve the hashed Dockerfiles against the project directory instead.**
  This closes the same gap from the other side and needs no override file. It
  makes skipper consistent — and consistently blind: the update in the clone
  would no longer be *seen*, so nothing would deploy and nothing would be
  reported. It also puts a hashed input outside the clone, against Invariant 1.
- **Run the build without `--project-directory`, from the compose file's own
  directory.** The simplest way to get the context right, but it changes the
  project name compose derives, which for a build-only service (no `image:`)
  also changes the tag of the image it produces — the following `up` would then
  look for an image the build never tagged. Correct only by coincidence, when
  both directories share a basename.
- **Copy the clone's Dockerfiles into the project directory before building.**
  Rejected: skipper would be writing into the operator's tree to work around
  path resolution, and a partial copy leaves the two trees disagreeing in a new
  way.
