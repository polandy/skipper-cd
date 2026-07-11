# UI end-to-end tests (Playwright)

Drives the embedded skipper-cd web UI in a real browser against the **real
skipper binary**, backed by a local git origin and a stub `docker` on `PATH`
(no docker daemon). The Node harness in `fixtures/harness.ts` is a twin of the
Go pipeline harness (`../harness_test.go`) — keep the stub docker script and
config shape in sync. Full spec: [`../../docs/e2e-tests.md`](../../docs/e2e-tests.md).

## Run

```sh
cd e2e/ui
npm ci
npx playwright install chromium   # first time only
npx playwright test
```

`globalSetup` builds the skipper binary once (`go build` from the repo root), so
Go and git must be on `PATH`. Set `SKIPPER_E2E_BIN` to reuse a prebuilt binary.

### NixOS

Playwright's bundled Chromium is dynamically linked and will not launch on NixOS.
Point the suite at a nix-provided Chromium instead:

```sh
export PW_CHROMIUM_EXECUTABLE="$(nix build nixpkgs#chromium --no-link --print-out-paths)/bin/chromium"
npx playwright test
```

`PW_CHROMIUM_EXECUTABLE` is unset in CI, which uses the pinned bundled browser.

## Layout

- `fixtures/harness.ts` — starts/stops a real skipper instance (origin, stub
  docker, free ports); `hold()`/`release()` gate a `compose up` so the
  `deploying` state is observable.
- `fixtures/test.ts` — the `skipper` test fixture (fresh instance per test).
- `tests/` — specs per UI mask (`ua-*` Deploys, `ub-*` Logs, `uc-*` Autosync,
  `ud-*` chrome).

Tests select **only** on the `data-testid` hooks documented in
[`../../internal/ui/UI_SPEC.md`](../../internal/ui/UI_SPEC.md).
