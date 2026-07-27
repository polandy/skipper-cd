import { test, expect } from '../fixtures/test';
import { buildCommit, buildVersion, repoRoot, type Skipper } from '../fixtures/harness';
import { execFileSync } from 'node:child_process';
import { mkdirSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, dirname } from 'node:path';
import type { Page } from '@playwright/test';

// Maske E: PWA update banner. See dev-docs/e2e-tests.md §4.6 and ADR-0023.
//
// The banner fires when the browser installs a *newer* service worker while an
// old one still controls the page. The only thing that makes a worker "newer"
// is its bytes changing — the app bakes the build identity into the sw.js cache
// name (__VERSION__ → BuildInfo.CacheID). So this suite runs a second skipper
// built with a different version/commit on the *same* origin and ports: after a
// relaunch onto it, the browser sees a new worker and the client flow we own
// (waiting worker → banner → SKIP_WAITING → controllerchange → one reload) runs
// for real. The server-side __VERSION__ substitution itself is covered by the
// ui handler unit tests.

const UPDATED_VERSION = '20.20.20';
const UPDATED_COMMIT = 'ba5eba11c0de';

const OLD_LABEL = `v${buildVersion} · ${buildCommit}`;
const NEW_LABEL = `v${UPDATED_VERSION} · ${UPDATED_COMMIT}`;

// A stack-free instance: the PWA surface needs no deploys, so this stays fast
// and docker-free (the startup deploy loop is empty).
test.use({ startOptions: { stacks: [] } });

let updatedBin = '';
let goAvailable = true;

test.beforeAll(() => {
  // The updated binary is built with go, exactly like globalSetup. When go is
  // unavailable (e.g. the snapshot-regeneration container, which mounts a
  // prebuilt SKIPPER_E2E_BIN instead) the update cannot be produced, so the
  // cases skip rather than fail — they carry no snapshots, so that run loses
  // nothing.
  try {
    execFileSync('go', ['version'], { stdio: 'ignore' });
  } catch {
    goAvailable = false;
    return;
  }
  // Worker-indexed path so parallel workers never write the same output file.
  updatedBin = join(tmpdir(), 'skipper-ui-e2e-bin', `skipper-updated-${process.env.TEST_WORKER_INDEX ?? '0'}`);
  mkdirSync(dirname(updatedBin), { recursive: true });
  execFileSync(
    'go',
    [
      'build',
      '-buildvcs=false',
      '-ldflags',
      `-X main.version=${UPDATED_VERSION} -X main.commit=${UPDATED_COMMIT}`,
      '-o',
      updatedBin,
      './cmd/skipper',
    ],
    { cwd: repoRoot(), stdio: 'inherit' },
  );
});

/** Waits until a service worker controls the page — the precondition for the
 *  banner (a new worker only prompts when one already controls the page). */
async function waitForController(page: Page): Promise<void> {
  await page.waitForFunction(() => navigator.serviceWorker.controller !== null, null, {
    timeout: 20_000,
  });
}

/** Drives the shared journey up to the banner: open the app on the shipped
 *  build (no banner, old version), then deploy the updated build and ask the
 *  page to check for it (mirroring the on-load / visibility update poll). */
async function deployUpdate(page: Page, skipper: Skipper) {
  await page.goto(`${skipper.baseURL}/`);
  await waitForController(page);

  const banner = page.locator('[data-testid="update-banner"]');
  const version = page.locator('[data-testid="brand-version"]');
  // First install: the app runs the shipped build with no prompt.
  await expect(version).toHaveText(OLD_LABEL);
  await expect(banner).toBeHidden();

  // A new version is deployed on the same origin/ports.
  await skipper.stop();
  await skipper.relaunch(updatedBin);

  // The same registration.update() the app polls on load / visibility regain.
  await page.evaluate(async () => {
    const reg = await navigator.serviceWorker.getRegistration();
    await reg?.update();
  });

  await expect(banner).toBeVisible();
  await expect(banner).toContainText('A new version of skipper-cd is available.');
  return { banner, version };
}

// UE1 — Accept. Tapping Reload activates the waiting worker and the page
// reloads once onto the new build (the header version flips old → new).
test('UE1: the update banner reloads onto the new version', async ({ page, skipper }) => {
  test.skip(!goAvailable, 'go toolchain unavailable — cannot build the updated binary');

  const { banner, version } = await deployUpdate(page, skipper);

  await page.locator('[data-testid="update-banner-reload"]').click();

  // controllerchange → one reload → the fresh shell reports the new build.
  await expect(version).toHaveText(NEW_LABEL);
  await expect(banner).toBeHidden();
});

// UE2 — Dismiss. The × hides the banner and keeps the current version: no
// SKIP_WAITING, so the worker stays waiting and the page never reloads.
test('UE2: dismissing the banner keeps the current version', async ({ page, skipper }) => {
  test.skip(!goAvailable, 'go toolchain unavailable — cannot build the updated binary');

  const { banner, version } = await deployUpdate(page, skipper);

  await page.locator('[data-testid="update-banner-close"]').click();

  await expect(banner).toBeHidden();
  // Still on the old build — the page did not reload.
  await expect(version).toHaveText(OLD_LABEL);
});
