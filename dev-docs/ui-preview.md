# UI preview

Status: living reference — recipe doc.

A one-command, seeded skipper-cd instance for **manually eyeballing the web UI**
— the manual-test-first gate every UI change goes through before its Playwright
mask is finalized.

```sh
make ui-preview            # http://127.0.0.1:3000/
PORT=8099 make ui-preview  # or pick another port
make ui-preview-smoke      # boot, assert, clean up, exit — no server
```

It builds the binary from the current checkout, stands up a throwaway origin
repo + a stub `docker` + a config, launches skipper, seeds a representative
spread of states, prints the URL, and stays up until `Ctrl-C` (cleaning up its
temp dir on exit). No docker daemon, no network, no `node_modules` — just Node,
git, and the Go toolchain (all in `nix develop`).

The server binds on all interfaces, so a phone/tablet on the same network (or
the same tunnel) can open `http://<host>:<port>/` too.

## What it seeds

Ten stacks, each with a coloured icon, arranged so that **every state the UI can
render is reachable** — the point being that a UI change to a failure path can be
eyeballed here, not only asserted in the e2e suite.

**Deploy outcomes** (Deploys view):

| Stack | Outcome | What it shows |
| --- | --- | --- |
| `nextcloud` | `success` | single-service tag bump; hooks + a deploy-health gate |
| `immich` | `success` | four services changed across two commits — the multi-commit diff head and the per-service version delta |
| `paperless` | `success` | same-tag rebuild: only the pinned digest moves |
| `gitea` | `success` | deployed and green, but its container is degraded — the ADR-0027 case a health pill exists for |
| `vaultwarden` | `rolled_back` | its `up` fails once, the rollback restores the previous compose — error panel + diff |
| `wiki` | `failed` | `up` always fails and `rollback: false`, so the failed containers are deliberately left running |
| `backup` | `blocked` | `depends_on: vaultwarden`, held back in the same run |
| `monitoring` | `queued` | paused through the same API the autosync drawer calls, then changed |
| `syncthing` | `healed` | seeded degraded, so self-heal runs a corrective redeploy — heal badge + drift panel |
| `experiments` | *(parked)* | `disabled: true` — present in the repo, never deployed |

`queued` and `blocked` are deliberately **not** written to the durable audit log,
so those two appear in the deploy log but their roster rows keep their previous
outcome. That is the product behaviour, not a seeding gap.

**Other surfaces:** health pills and the containers panel (healthy / unhealthy /
stopped, each with its running image, so version cells populate), the attention
band + health beacon, the status timeline, orphan detection (a removed stack
still running, plus an unmanaged project), Traefik app links (one host and a
two-host popover), a second host fanned in over `peers:` plus one that is
unreachable, the autosync drawer with a pending count, the logs view, and the
theme switcher.

**Reconcile is off** (`reconcile_interval_seconds: 0`). The failure states are
transient in product terms — a rolled-back stack stays dirty and retries, a
blocked one unblocks once its dependency succeeds — so a reconcile tick would
resolve them a few minutes in and leave whoever opens the preview later looking
at an all-green board. Deploys here come from the seeding's own webhooks
instead, which keeps the spread stable for the session. Push to the throwaway
origin yourself and hit `/webhook` if you want another run.

**Not reachable here:** `rolled_back_unhealthy` and `heal_exhausted`. Both need a
stack to keep failing across several attempts, which would make the fixture slow
and noisy for two badges; the e2e masks cover them.

## The smoke run

`make ui-preview-smoke` (`--smoke`) seeds exactly as above, then asserts and
exits instead of serving: the roster has every declared stack, the audit-recorded
outcomes include `success`, `rolled_back`, `failed` and `healed`, and the pending
registry holds one blocked and one per-stack-paused entry. Non-zero on any miss.

CI runs it on **every** push, with no path filter. It is skipper's startup smoke
test first — nothing else asserts that the binary boots and serves with a
full-featured config (discovery, peers, hooks, icons, health watch and self-heal
together) — and the guard on this fixture second. The filter is deliberately
absent: the last time this fixture broke it was a key rename in
`internal/config`, not an edit to the script, so anything narrower would have
missed it.

## Relationship to the e2e harness

This script (`scripts/ui-preview.mjs`) is a deliberately self-contained twin of
the Playwright launcher, `e2e/ui/fixtures/harness.ts`. That harness — run
through Playwright's TypeScript loader — stays the authoritative, asserted way
skipper is booted for tests. The preview trades a little duplication for **zero
toolchain dependencies**, so it runs anywhere with plain `node`.

The two configs are **not** shared, and deliberately so: the harness's is
option-driven (one shape per mask, ~30 toggles), this one is a single fixed
scenario. A shared builder would be the union of both, re-implementing in JS what
`internal/config` already validates. What keeps this one honest instead is that
the preview runs `skipper -validate` against its generated config before booting,
so a renamed or mistyped key fails here with one actionable line — the check that
replaced the old "keep the two in rough sync" instruction, which is exactly what
rotted.
