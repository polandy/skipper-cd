import { test, expect } from '../fixtures/test';
import type { Page } from '@playwright/test';

// Maske AO: registry update check (ADR-0054). See dev-docs/e2e-tests.md §4.41.
//
// The read-only check compares what each service runs against what its
// registry offers and annotates the Stacks view's version chips: an amber
// `⇡ <tag>` for a newer same-shape tag, `⇡ rebuilt` when the running tag's
// upstream digest moved, and a `⇡ N updates · checked …` summary in the
// containers-panel head. The harness stands up a local registry stub and the
// stub docker answers the `image inspect` half, so the whole pipeline —
// running images → checker → stacks snapshot → chips — runs for real. Covers
// the newer-tag row chip (UAO1), the panel markers + head summary (UAO2), the
// rebuilt form (UAO3) and the unmarked control stack (UAO4).

const UPSTREAM_DIGEST = 'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa';
const OLD_LOCAL_DIGEST = 'sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb';

test.use({
  startOptions: {
    stacks: ['gitea', 'proxy', 'plain'],
    healthPoll: 1,
    initialCompose: {
      gitea: 'services:\n  server:\n    image: {{registry}}/gitea/gitea:1.22.3\n',
      proxy: 'services:\n  traefik:\n    image: {{registry}}/traefik:v3.1\n',
      plain: 'services:\n  app:\n    image: {{registry}}/plain:1.0.0\n',
    },
    initialHealth: {
      gitea: [
        {
          Service: 'server',
          Name: 'gitea-server-1',
          Image: '{{registry}}/gitea/gitea:1.22.3',
          State: 'running',
          Health: 'healthy',
        },
      ],
      proxy: [
        {
          Service: 'traefik',
          Name: 'proxy-traefik-1',
          Image: '{{registry}}/traefik:v3.1',
          State: 'running',
          Health: 'healthy',
        },
      ],
      plain: [
        {
          Service: 'app',
          Name: 'plain-app-1',
          Image: '{{registry}}/plain:1.0.0',
          State: 'running',
          Health: 'healthy',
        },
      ],
    },
    updateCheck: {
      tags: {
        // gitea: a newer same-shape tag exists → `1.22.3 ⇡ 1.22.6`.
        'gitea/gitea': ['1.21.0', '1.22.3', '1.22.6', 'latest'],
        // proxy: nothing newer, so the digest decides → `v3.1 ⇡ rebuilt`.
        traefik: ['v3.0', 'v3.1'],
        // plain: nothing newer and the digest matches → no marker.
        plain: ['1.0.0'],
      },
      digest: UPSTREAM_DIGEST,
      repoDigests: {
        '{{registry}}/traefik:v3.1': [`{{registry}}/traefik@${OLD_LOCAL_DIGEST}`],
        '{{registry}}/plain:1.0.0': [`{{registry}}/plain@${UPSTREAM_DIGEST}`],
      },
    },
  },
});

const stacksBtn = (page: Page) => page.locator('[data-testid="view-toggle"] button[data-view="stacks"]');
const row = (page: Page, stack: string) => page.locator(`[data-testid="roster-row"][data-stack="${stack}"]`);
const versionCell = (page: Page, stack: string) => row(page, stack).locator('[data-testid="roster-version"]');

// Open the Stacks view. The check runs right after the startup deploys (the
// post-run nudge), so the marker is an SSE republish away — awaited per case,
// never assumed present at navigation time.
async function openStacks(page: Page, baseURL: string): Promise<void> {
  await page.goto(`${baseURL}/`);
  await stacksBtn(page).click();
  await expect(page.locator('[data-testid="stacks-view"]')).toBeVisible();
}

// UAO1 — a newer same-shape tag reads as `running ⇡ latest` on the row chip.
test('UAO1: a roster row chip carries the amber marker for a newer tag', async ({ page, skipper }) => {
  await openStacks(page, skipper.baseURL);
  const chip = versionCell(page, 'gitea').locator('.tag-delta');
  await expect(chip.locator('.td-upd')).toHaveText('⇡1.22.6');
  await expect(chip.locator('.td-cur')).toHaveText('1.22.3');
  // The tooltip carries the full story the compact chip drops.
  await expect(chip).toHaveAttribute('title', /upstream 1\.22\.6 available/);
});

// UAO2 — the containers panel marks the affected service and its head sums up
// the stack's updates with the check's freshness.
test('UAO2: the containers panel carries per-service markers and the head summary', async ({ page, skipper }) => {
  await openStacks(page, skipper.baseURL);
  await expect(versionCell(page, 'gitea').locator('.td-upd')).toBeVisible();
  await row(page, 'gitea').click();
  const panel = page.locator('[data-testid="health-panel"]');
  await expect(panel).toBeVisible();
  const meta = panel.locator('[data-testid="update-check-meta"]');
  await expect(meta).toHaveText(/⇡ 1 update · checked .+ ago/);
  await expect(
    panel.locator('[data-testid="health-service"]').filter({ hasText: 'server' }).locator('.td-upd'),
  ).toHaveText('⇡1.22.6');
});

// UAO3 — a running tag whose upstream digest moved reads as rebuilt, mirroring
// the Deploys column's same-tag form.
test('UAO3: a republished tag reads as ⇡ rebuilt', async ({ page, skipper }) => {
  await openStacks(page, skipper.baseURL);
  const chip = versionCell(page, 'proxy').locator('.tag-delta');
  await expect(chip.locator('.td-upd')).toHaveText('⇡rebuilt');
  await expect(chip).toHaveAttribute('title', /rebuilt upstream/);
});

// UAO4 — a current stack stays exactly as it was before the feature existed:
// no marker on the chip, no summary in the panel head.
test('UAO4: an up-to-date stack shows no marker and no head summary', async ({ page, skipper }) => {
  await openStacks(page, skipper.baseURL);
  // Positive signal first: the check has run (gitea is marked) — only then is
  // plain's absence meaningful rather than a not-yet-checked false green.
  await expect(versionCell(page, 'gitea').locator('.td-upd')).toBeVisible();
  await expect(versionCell(page, 'plain').locator('.tag-delta')).toBeVisible();
  await expect(versionCell(page, 'plain').locator('.td-upd')).toHaveCount(0);
  await row(page, 'plain').click();
  const panel = page.locator('[data-testid="health-panel"]');
  await expect(panel).toBeVisible();
  await expect(panel.locator('[data-testid="update-check-meta"]')).toHaveCount(0);
});
