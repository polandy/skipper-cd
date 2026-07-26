import { test, expect } from '../fixtures/test';
import type { Locator, Page } from '@playwright/test';

// Maske AK: per-service versions in the Stacks view. See dev-docs/e2e-tests.md
// §4.37.
//
// `compose ps` reports the image each container runs, which rides the health
// snapshot: a roster row's Version cell names the service the stack is named
// after plus its running version (and a "+N" for the rest), and the expanded
// containers panel carries every service's version — the same chip the Deploys
// Version column renders (Maske AI). Covers the row cell (UAK1), the no-lead
// fallback (UAK2), the panel column (UAK3), a snapshot without images — which must
// degrade to no version at all rather than an empty column (UAK4) — the live
// refresh in place (UAK5) and the narrow-viewport layout (UAK6).

test.use({
  startOptions: {
    stacks: ['immich', 'monitoring', 'legacy'],
    healthPoll: 1,
    initialHealth: {
      // Two services mention the stack; the shorter name leads.
      immich: [
        { Service: 'database', Image: 'ghcr.io/tensorchord/pgvecto-rs:pg16-v0.3.0', State: 'running' },
        {
          Service: 'immich-machine-learning',
          Image: 'ghcr.io/immich-app/immich-machine-learning:v1.119.0',
          State: 'running',
        },
        { Service: 'immich-server', Image: 'ghcr.io/immich-app/immich-server:v1.119.0', State: 'running' },
      ],
      // A role-named stack: no service is "the" one, so the row only counts them.
      monitoring: [
        { Service: 'prometheus', Image: 'prom/prometheus:v3.0.0', State: 'running', Health: 'healthy' },
        { Service: 'grafana', Image: 'grafana/grafana:11.3.0', State: 'running', Health: 'healthy' },
      ],
      // A snapshot carrying no image at all (an older skipper, or a peer of one).
      legacy: [{ Service: 'app', State: 'running', Health: 'healthy' }],
    },
  },
});

const stacksBtn = (page: Page) => page.locator('[data-testid="view-toggle"] button[data-view="stacks"]');
const row = (page: Page, stack: string) => page.locator(`[data-testid="roster-row"][data-stack="${stack}"]`);
const versionCell = (page: Page, stack: string) => row(page, stack).locator('[data-testid="roster-version"]');

// Open the Stacks view with the health snapshot already landed, so the version
// cells are populated (the roster renders from the stacks snapshot, the versions
// arrive with health).
async function openStacks(page: Page, baseURL: string): Promise<void> {
  await page.goto(`${baseURL}/`);
  await expect(page.locator('[data-testid="health-pill"]').first()).toBeVisible();
  await stacksBtn(page).click();
  await expect(page.locator('[data-testid="stacks-view"]')).toBeVisible();
}

// UAK1 — the row leads with the service the stack is named after, its running
// version, and how many services it stands for.
test('UAK1: a roster row shows its lead service version and a count for the rest', async ({ page, skipper }) => {
  await openStacks(page, skipper.baseURL);

  // The aligned column header gained the Version column.
  await expect(page.locator('.roster-list-header')).toContainText('Version');

  const cell = versionCell(page, 'immich');
  const chip = cell.locator('.tag-delta');
  await expect(chip).toHaveCount(1);
  // immich-server wins over immich-machine-learning (shorter name), and never
  // `database` — `compose ps` order is alphabetical and must not decide.
  await expect(chip.locator('.td-svc')).toHaveText('immich-server');
  await expect(chip.locator('.td-cur')).toHaveText('v1.119.0');
  // A running version is a fact, not a change: no old→new arrow on this chip.
  await expect(chip.locator('.td-arr')).toHaveCount(0);
  // Screen readers get the whole phrase; the title carries the full reference the
  // visible token drops (registry + repository).
  await expect(chip).toHaveAttribute('aria-label', 'immich-server running v1.119.0');
  await expect(chip).toHaveAttribute('title', 'immich-server: ghcr.io/immich-app/immich-server:v1.119.0');
  // Three services, one named → "+2" pointing at the panel.
  await expect(cell.locator('.ver-count')).toHaveText('+2');

  // It is its own column between Stack and Status, so the versions line up down
  // the page rather than riding inside the stack cell.
  const stackCell: Locator = row(page, 'immich').locator('.roster-stack');
  const [stackBox, verBox, statusBox] = await Promise.all([
    stackCell.boundingBox(),
    cell.boundingBox(),
    row(page, 'immich').locator('.roster-status').boundingBox(),
  ]);
  expect(verBox!.x).toBeGreaterThan(stackBox!.x);
  expect(statusBox!.x).toBeGreaterThan(verBox!.x);
});

// UAK2 — a stack whose name identifies none of its services gets no arbitrary
// pick: the cell reports the count and defers to the panel.
test('UAK2: a stack with no lead service shows only its service count', async ({ page, skipper }) => {
  await openStacks(page, skipper.baseURL);

  const cell = versionCell(page, 'monitoring');
  await expect(cell.locator('.tag-delta')).toHaveCount(0);
  await expect(cell.locator('.ver-count')).toHaveText('2 services');
});

// UAK3 — expanding a stack lists every service's running version in the
// containers panel, beside the state and status it already showed.
test('UAK3: the containers panel shows every service version', async ({ page, skipper }) => {
  await openStacks(page, skipper.baseURL);
  await row(page, 'immich').click();

  const panel = page.locator('[data-testid="health-panel"]');
  await expect(panel).toHaveCount(1);
  await expect(panel).toHaveClass(/has-versions/);
  const versions = panel.locator('[data-testid="health-version"]');
  await expect(versions).toHaveCount(3);
  // Same order as the service lines; each names its version, and the line — not
  // the chip — carries the service name, so it is not repeated.
  await expect(versions.nth(0)).toHaveText('pg16-v0.3.0');
  await expect(versions.nth(2)).toHaveText('v1.119.0');
  await expect(versions.nth(2).locator('.td-svc')).toHaveCount(0);
  await expect(versions.nth(2).locator('.tag-delta')).toHaveAttribute(
    'title',
    'immich-server: ghcr.io/immich-app/immich-server:v1.119.0',
  );

  // The panel stays the row's bound sibling — versions are an addition to the
  // existing service lines, not a replacement.
  await expect(panel.locator('[data-testid="health-service"]')).toHaveCount(3);
});

// UAK4 — a health snapshot without images (an older skipper, a peer of one)
// shows no version anywhere, rather than empty chips or an empty column.
test('UAK4: a snapshot carrying no images shows no version at all', async ({ page, skipper }) => {
  await openStacks(page, skipper.baseURL);

  await expect(versionCell(page, 'legacy')).toBeEmpty();

  await row(page, 'legacy').click();
  const panel = page.locator('[data-testid="health-panel"]');
  await expect(panel.locator('[data-testid="health-service"]')).toHaveCount(1);
  await expect(panel).not.toHaveClass(/has-versions/);
  await expect(panel.locator('[data-testid="health-version"]')).toHaveCount(0);
});

// UAK5 — the versions ride the health snapshot, so a later poll refreshes the
// cell in place: an open panel survives and the new version shows without a
// re-render.
test('UAK5: a new image on the next poll updates the cell without dropping a panel', async ({ page, skipper }) => {
  await openStacks(page, skipper.baseURL);
  await row(page, 'immich').click();
  await expect(page.locator('[data-testid="health-panel"]')).toHaveCount(1);

  skipper.setStackHealth('immich', [
    { Service: 'immich-server', Image: 'ghcr.io/immich-app/immich-server:v1.120.0', State: 'running' },
  ]);

  const cell = versionCell(page, 'immich');
  await expect(cell.locator('.td-cur')).toHaveText('v1.120.0');
  // One service left, so nothing is deferred to the panel any more.
  await expect(cell.locator('.ver-count')).toHaveCount(0);
  // The open panel was never dropped by the patch-in-place.
  await expect(page.locator('[data-testid="health-panel"]')).toHaveCount(1);
  await expect(row(page, 'immich')).toHaveClass(/audit-open/);
});

// UAK6 — no room for five columns below 1000 px: the cell drops to its own line
// under the row rather than squeezing the stack name away.
test('UAK6: the version drops to its own line on a narrow viewport', async ({ page, skipper }) => {
  await page.setViewportSize({ width: 820, height: 900 });
  await openStacks(page, skipper.baseURL);

  // The column header sheds its Version label, so the remaining four stay in
  // lockstep with the rows.
  await expect(page.locator('.roster-list-header .rh-ver')).toBeHidden();

  const stackCell = row(page, 'immich').locator('.roster-stack');
  const cell = versionCell(page, 'immich');
  const [stackBox, verBox] = await Promise.all([stackCell.boundingBox(), cell.boundingBox()]);
  // Below the name, not beside it — and starting at the same left edge.
  expect(verBox!.y).toBeGreaterThan(stackBox!.y + stackBox!.height - 2);
  expect(Math.abs(verBox!.x - stackBox!.x)).toBeLessThan(2);
  // The stack name still renders in full (nothing was squeezed away for it).
  await expect(row(page, 'immich').locator('.roster-name')).toHaveText('immich');
});
