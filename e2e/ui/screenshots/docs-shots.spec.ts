import { mkdirSync } from 'node:fs';
import { join } from 'node:path';
import { test, expect } from '../fixtures/test';
import type { Page } from '@playwright/test';

// Renders the two screenshots the docs landing page embeds
// (docs/assets/screenshots/{deploys,stacks}.png). They are **generated, not
// committed**: a screenshot of the UI is a derived artifact, and a PNG never
// delta-compresses, so committing one on every UI change would grow the repo
// without bound. CI renders them before `mkdocs build` (.github/workflows/
// docs.yml); locally `make docs-screenshots` does the same.
//
// This is a renderer, not a test: it asserts only enough to know the page is
// ready to photograph. Both shots come from ONE instance in one test, because
// the Stacks view's Commit column only fills for webhook deploys — the deploy
// history has to be staged first, and the roster then inherits it.

const OUT = join(__dirname, '..', '..', '..', 'docs', 'assets', 'screenshots');

// Realistic self-hosted stacks. The names are dashboard-icons slugs, so each
// resolves to that project's real logo (fetched below).
const STACKS = ['immich', 'nextcloud', 'jellyfin', 'home-assistant', 'vaultwarden', 'pi-hole'];

// What each stack declares — real images, so a diff and a version chip read like
// a homelab rather than nginx:1.25.
const COMPOSE: Record<string, Record<string, string>> = {
  immich: {
    'immich-server': 'ghcr.io/immich-app/immich-server:v1.118.2',
    'immich-machine-learning': 'ghcr.io/immich-app/immich-machine-learning:v1.118.2',
    redis: 'redis:7.4',
    database: 'ghcr.io/tensorchord/pgvecto-rs:pg16-v0.3.0',
  },
  nextcloud: { app: 'nextcloud:30.0.2', db: 'postgres:16', redis: 'redis:7.2' },
  jellyfin: { jellyfin: 'jellyfin/jellyfin:10.10.3' },
  'home-assistant': { homeassistant: 'ghcr.io/home-assistant/home-assistant:2024.11.3' },
  vaultwarden: { vaultwarden: 'vaultwarden/server:1.32.5' },
  'pi-hole': { pihole: 'pihole/pihole:2024.07.0' },
};

// The containers the health poller reports — what the Stacks view shows as each
// service's running version. Mirrors COMPOSE; the bumps below update both sides.
const RUNNING: Record<string, Array<{ Service: string; Image: string; State: string; Health: string }>> =
  Object.fromEntries(
    Object.entries(COMPOSE).map(([stack, svcs]) => [
      stack,
      Object.entries(svcs).map(([Service, Image]) => ({
        Service,
        Image,
        State: 'running',
        Health: 'healthy',
      })),
    ]),
  );

// Versions the staged pushes land, in push order. immich leads the Deploys shot
// (its diff is the focal point); nextcloud is held mid-deploy for the `deploying`
// row; the rest bring every roster row a commit for the Stacks shot.
const IMMICH_BUMP = 'v1.119.0';
const NEXTCLOUD_BUMP = '30.0.3';
const REMAINING_BUMPS: Record<string, string> = {
  'home-assistant': '2024.12.1',
  jellyfin: '10.10.4',
  'pi-hole': '2024.07.1',
  vaultwarden: '1.32.7',
};

const composeYaml = (svcs: Record<string, string>) =>
  'services:\n' +
  Object.entries(svcs)
    .map(([svc, image]) => `  ${svc}:\n    image: ${image}\n`)
    .join('');

// Real project logos, fetched from the icon set skipper itself auto-matches
// against. Seeded as each stack's repo `icon.svg` (the harness runs offline, with
// no icon CDN of its own). A fetch failure is not fatal: the row falls back to
// skipper's monogram chip, which is a fine screenshot too — a CDN hiccup must
// never break the docs build.
const ICON_BASE = 'https://cdn.jsdelivr.net/gh/homarr-labs/dashboard-icons/svg';

let icons: Record<string, string> = {};

async function fetchIcons(): Promise<Record<string, string>> {
  const fetched: Record<string, string> = {};
  await Promise.all(
    STACKS.map(async (name) => {
      try {
        const resp = await fetch(`${ICON_BASE}/${name}.svg`, { signal: AbortSignal.timeout(10_000) });
        if (resp.ok) fetched[name] = await resp.text();
      } catch {
        // fall through to the monogram
      }
    }),
  );
  const missing = STACKS.filter((n) => !fetched[n]);
  if (missing.length) console.warn(`[docs-shots] no logo for ${missing.join(', ')} — using monograms`);
  return fetched;
}

// The logos are fetched, so the start options are built in a fixture rather than
// through test.use() (a plain value cannot await).
const shot = test.extend<Record<string, never>>({
  startOptions: async ({}, use) => {
    icons = await fetchIcons();
    await use({
      stacks: STACKS,
      initialCompose: Object.fromEntries(
        Object.entries(COMPOSE).map(([n, svcs]) => [n, composeYaml(svcs)]),
      ),
      // A Renovate-authored bump is the loop skipper exists for, and the diff
      // panel names the author.
      commitAuthor: { name: 'renovate[bot]', email: 'renovate@users.noreply.github.com' },
      healthPoll: 2,
      initialHealth: RUNNING,
      stackIcons: icons,
    });
  },
});

const rowsFor = (page: Page, stack: string) =>
  page.locator(`[data-testid="deploy-row"][data-stack="${stack}"]`);

const rosterRow = (page: Page, stack: string) =>
  page.locator(`[data-testid="roster-row"][data-stack="${stack}"]`);

// logosResolved waits until every stack chip **in frame** shows its logo instead
// of the monogram fallback. Only the visible ones: skipper lazy-loads the icon
// images, so a chip below the fold never reports complete — and it is not in the
// photo anyway. These SVGs carry only a viewBox, so naturalWidth stays 0; the
// chip's class (set on the img error path) is the signal. Never fatal — with no
// logos at all it returns immediately and the shot shows monograms.
async function logosResolved(page: Page): Promise<void> {
  if (Object.keys(icons).length === 0) return;
  await page
    .waitForFunction(
      () => {
        const inFrame = Array.from(document.querySelectorAll('[data-testid="stack-icon"]')).filter(
          (c) => {
            const r = c.getBoundingClientRect();
            return r.top < window.innerHeight && r.bottom > 0;
          },
        );
        return (
          inFrame.length > 0 &&
          inFrame.every((c) => !c.classList.contains('mono') && !!c.querySelector('img')?.complete)
        );
      },
      undefined,
      { timeout: 20_000 },
    )
    .catch(() => console.warn('[docs-shots] some logos did not resolve — shooting anyway'));
}

shot('render the docs landing-page screenshots', async ({ page, skipper }) => {
  mkdirSync(OUT, { recursive: true });
  await page.goto(`${skipper.baseURL}/`);

  // Startup deploys: one row per stack, then the first health poll lands.
  for (const s of STACKS) await expect(rowsFor(page, s)).toHaveCount(1);
  await expect(page.locator('[data-testid="health-pill"]').first()).toBeVisible();
  await logosResolved(page);

  // A single version bump — its row's diff is the focal point of the shot.
  skipper.setStackImage('immich', IMMICH_BUMP);
  expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);
  await expect(rowsFor(page, 'immich')).toHaveCount(2);
  await rowsFor(page, 'immich').first().locator('[data-testid="files-pill"]').click();
  await expect(page.locator('[data-testid="diff-panel"]')).toBeVisible();

  // A second push, held at `compose up`, so the top row stays `deploying` for the
  // shot and the header shows the live deploy indicator.
  skipper.hold();
  skipper.setStackImage('nextcloud', NEXTCLOUD_BUMP);
  expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);
  await expect(
    page.locator('[data-testid="deploy-row"][data-stack="nextcloud"][data-status="deploying"]'),
  ).toHaveCount(1);

  await page.setViewportSize({ width: 1320, height: 980 });
  await page.screenshot({ path: join(OUT, 'deploys.png') });

  // Let the held deploy finish, then bring the remaining stacks up to date, so
  // the inventory shot has a commit on every row.
  skipper.release();
  await expect(
    page.locator('[data-testid="deploy-row"][data-stack="nextcloud"][data-status="success"]').first(),
  ).toBeVisible();

  for (const [stack, tag] of Object.entries(REMAINING_BUMPS)) skipper.setStackImage(stack, tag);
  expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);
  for (const s of Object.keys(REMAINING_BUMPS)) await expect(rowsFor(page, s)).toHaveCount(2);

  // The containers now run what the repo declares, so the inventory's versions
  // agree with its commits. Every bumped stack, immich included: its chip would
  // otherwise show the pre-bump version right next to its successful bump.
  for (const [stack, tag] of Object.entries({
    ...REMAINING_BUMPS,
    immich: IMMICH_BUMP,
    nextcloud: NEXTCLOUD_BUMP,
  })) {
    const svcs = RUNNING[stack].map((c, i) =>
      i === 0 ? { ...c, Image: c.Image.replace(/:[^:/]+$/, `:${tag}`) } : c,
    );
    skipper.setStackHealth(stack, svcs);
  }

  await page.locator('[data-testid="view-toggle"] button[data-view="stacks"]').click();
  await expect(page.locator('[data-testid="stacks-view"]')).toBeVisible();
  await expect(page.locator('[data-testid="roster-row"]')).toHaveCount(STACKS.length);
  // Versions arrive with the health snapshot, a beat after the roster renders.
  await expect(page.locator('[data-testid="roster-version"] .tag-delta')).toHaveCount(STACKS.length);
  await expect(rosterRow(page, 'jellyfin').locator('.td-cur')).toHaveText(REMAINING_BUMPS.jellyfin);
  await expect(rosterRow(page, 'nextcloud').locator('.td-cur')).toHaveText(NEXTCLOUD_BUMP);
  await expect(rosterRow(page, 'immich').locator('.td-cur')).toHaveText(IMMICH_BUMP);
  await expect(page.locator('[data-testid="roster-row"] [data-testid="health-pill"]').first()).toBeVisible();
  await logosResolved(page);

  await page.setViewportSize({ width: 1320, height: 600 });
  await page.screenshot({ path: join(OUT, 'stacks.png') });
});
