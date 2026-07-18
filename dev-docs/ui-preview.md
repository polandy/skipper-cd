# UI preview

A one-command, seeded skipper-cd instance for **manually eyeballing the web UI**
— the manual-test-first gate every UI change goes through before its Playwright
mask is finalized.

```sh
make ui-preview            # http://127.0.0.1:3000/
PORT=8099 make ui-preview  # or pick another port
```

It builds the binary from the current checkout, stands up a throwaway origin
repo + a stub `docker` + a config, launches skipper, seeds a representative
spread of states, prints the URL, and stays up until `Ctrl-C` (cleaning up its
temp dir on exit). No docker daemon, no network, no `node_modules` — just Node,
git, and the Go toolchain (all in `nix develop`).

The server binds on all interfaces, so a phone/tablet on the same network (or
the same tunnel) can open `http://<host>:<port>/` too.

## What it seeds

- Four stacks (`web`, `api`, `worker`, `database`), each with a coloured icon.
- A pushed change to `web` (single-commit diff) and to `api` (two commits → the
  multi-commit diff head with the "N commits" pill).
- Health poll + watch on: `web`/`api` healthy, `worker` degraded, `database`
  stopped — so the health pills, the health panel, and the status timeline all
  render with variety.
- The autosync drawer, the run/upcoming header, the logs view, and the theme
  switcher are all live.

## Relationship to the e2e harness

This script (`scripts/ui-preview.mjs`) is a deliberately self-contained twin of
the Playwright launcher, `e2e/ui/fixtures/harness.ts`. That harness — run
through Playwright's TypeScript loader — stays the authoritative, asserted way
skipper is booted for tests. The preview trades a little duplication for **zero
toolchain dependencies**, so it runs anywhere with plain `node`. Keep the config
shape here in rough sync with the harness's when the config changes.
