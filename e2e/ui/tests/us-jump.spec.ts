import { test, expect } from '../fixtures/test';
import type { Page, Locator } from '@playwright/test';

// Maske S: cross-view stack jump. See docs/dev/e2e-tests.md §4.20.
//
// The compass jump-btn — in the head of a row's detail panel, in both views —
// switches between the Deploys and Stacks views and lands on that stack's row
// there — Deploys -> Stacks always lands on the one roster row (inventory) and
// opens its detail; Stacks -> Deploys lands on the *newest* deploy row (the
// table is an event log, newest first). Behaviour-only (no snapshot).

const deploysBtn = (page: Page) => page.locator('[data-testid="view-toggle"] button[data-view="deploys"]');
const stacksBtn = (page: Page) => page.locator('[data-testid="view-toggle"] button[data-view="stacks"]');
const deployRows = (page: Page, stack: string) =>
  page.locator(`[data-testid="deploy-row"][data-stack="${stack}"]`);
const rosterRow = (page: Page, stack: string) =>
  page.locator(`[data-testid="roster-row"][data-stack="${stack}"]`);
const jumpBtnIn = (row: Locator) => row.locator('[data-testid="jump-btn"]');
const detailPanel = (page: Page, stack: string) =>
  page.locator(`[data-testid="stacks-view"] [data-testid="detail-panel"][data-stack="${stack}"]`);
const deployPanel = (page: Page, stack: string) =>
  page.locator(`[data-testid="detail-panel"][data-kind="deploy"][data-stack="${stack}"]`);
// Rows carry no controls: the jump is in the head of the row's detail panel,
// so opening the row comes first.
async function rosterJump(page: Page, stack: string): Promise<Locator> {
  await rosterRow(page, stack).click();
  return jumpBtnIn(detailPanel(page, stack));
}
async function deployJump(page: Page, stack: string): Promise<Locator> {
  await deployRows(page, stack).first().locator('.stack-name').click();
  return jumpBtnIn(deployPanel(page, stack));
}

test.describe('cross-view jump', () => {
  test.use({ startOptions: { stacks: ['api', 'web'] } });

  // US1 — a deploy detail's jump button switches to Stacks and lands on that
  // stack's roster row (flashed via .jump-target, cleared again after), its
  // detail open.
  test('US1: Deploys -> Stacks jump lands on and flashes the roster row', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);

    await (await deployJump(page, 'api')).click();

    await expect(page.locator('[data-testid="stacks-view"]')).toBeVisible();
    await expect(page.locator('[data-testid="deploys-table"]')).toBeHidden();
    await expect(stacksBtn(page)).toHaveClass(/active/);

    const target = rosterRow(page, 'api');
    await expect(target).toBeVisible();
    await expect(target).toHaveClass(/jump-target/);
    await expect(detailPanel(page, 'api')).toBeVisible();
    // The flash is temporary, not a persisted state like audit-open.
    await expect(target).not.toHaveClass(/jump-target/, { timeout: 3000 });
  });

  // US2 — a roster row's jump button (in its detail panel) switches to Deploys and lands on the
  // *newest* row for that stack, not an older one (the table is a log with
  // one row per deploy; the roster only knows one row per stack).
  test('US2: Stacks -> Deploys jump lands on the newest row, not an older one', async ({
    page,
    skipper,
  }) => {
    await page.goto(`${skipper.baseURL}/`);

    // A second deploy of `api`, so there are two rows to distinguish by
    // DOM order (newest first).
    skipper.setStackImage('api', '1.26');
    expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);
    await expect(deployRows(page, 'api')).toHaveCount(2);
    await expect(deployRows(page, 'api').first()).toHaveAttribute('data-status', 'success');

    await stacksBtn(page).click();
    await (await rosterJump(page, 'api')).click();

    await expect(page.locator('[data-testid="deploys-table"]')).toBeVisible();
    await expect(deploysBtn(page)).toHaveClass(/active/);
    await expect(deployRows(page, 'api').first()).toHaveClass(/jump-target/);
    await expect(deployRows(page, 'api').nth(1)).not.toHaveClass(/jump-target/);
  });

  // US3 — a jump only switches views: the detail panel it sits in is left as
  // it was — not closed, its tab unchanged — so the way back still shows it.
  test('US3: the jump leaves the panel it sits in open', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);

    await (await deployJump(page, 'api')).click();
    await expect(page.locator('[data-testid="stacks-view"]')).toBeVisible();
    await expect(deployPanel(page, 'api')).toHaveCount(1);

    // The jump opened api's Stacks detail; its own jump leads back.
    await jumpBtnIn(detailPanel(page, 'api')).click();
    await expect(page.locator('[data-testid="deploys-table"]')).toBeVisible();
    await expect(deployPanel(page, 'api')).toBeVisible();
    await expect(detailPanel(page, 'api')).toHaveCount(1);
    await expect(detailPanel(page, 'api')).toHaveAttribute('data-tab', 'triggers');
    await stacksBtn(page).click();
    await expect(detailPanel(page, 'api')).toBeVisible();
  });

  // US5 — a leftover search filter in the target view must not leave the
  // landing row `.filtered-out` (hidden): the jump switched views, but the
  // stack it promised to show would be invisible with no indication why.
  // Checked in both directions.
  test('US5: a leftover filter in the target view does not hide the landing row', async ({
    page,
    skipper,
  }) => {
    await page.goto(`${skipper.baseURL}/`);

    // Stacks -> Deploys: filter Deploys down to "web" (api hidden), then leave
    // it filtered and switch to Stacks via the view toggle (not a jump).
    // Wait for the rows first: type-to-search ignores keystrokes until a row
    // exists (the `!hasRows` guard), so typing before the SSE snapshot lands drops
    // the first key ("web" -> "eb") and 'api' stays visible — the US5 flake.
    await expect(deployRows(page, 'api').first()).toBeVisible();
    await page.keyboard.type('web');
    await expect(page.locator('[data-testid="deploy-filter"]')).toHaveValue('web');
    await expect(deployRows(page, 'api').first()).toBeHidden();
    await stacksBtn(page).click();

    // Jumping from api's roster row must clear that stale Deploys filter so
    // its landing row is actually visible.
    await (await rosterJump(page, 'api')).click();
    await expect(deployRows(page, 'api').first()).toBeVisible();
    await expect(deployRows(page, 'api').first()).toHaveClass(/jump-target/);
    await expect(page.locator('[data-testid="deploy-filter"]')).toHaveValue('');

    // Deploys -> Stacks: filter Stacks down to "web" (api hidden), leave it
    // filtered and switch back to Deploys via the toggle. Same rows-first guard.
    await stacksBtn(page).click();
    await expect(rosterRow(page, 'api')).toBeVisible();
    await page.keyboard.type('web');
    await expect(page.locator('[data-testid="roster-filter"]')).toHaveValue('web');
    await expect(rosterRow(page, 'api')).toBeHidden();
    await deploysBtn(page).click();

    // Jumping from api's deploy detail must clear that stale Stacks filter.
    await (await deployJump(page, 'api')).click();
    await expect(rosterRow(page, 'api')).toBeVisible();
    await expect(rosterRow(page, 'api')).toHaveClass(/jump-target/);
    await expect(page.locator('[data-testid="roster-filter"]')).toHaveValue('');
  });
});

// US4 — a stack the Deploys view has no row for yet (parked, never deployed)
// still carries a jump button in its detail panel; the jump degrades to a plain
// view switch with nothing to land on.
test.describe('US4: jump with no landing target', () => {
  test.use({
    startOptions: {
      stacks: ['web', 'wip'],
      discovery: { repoConfig: 'stacks:\n  wip:\n    disabled: true\n', disabled: ['wip'] },
    },
  });

  test('a parked stack has no deploy row, so its jump only switches views', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);
    await stacksBtn(page).click();
    await expect(rosterRow(page, 'wip')).toBeVisible();

    await (await rosterJump(page, 'wip')).click();

    await expect(page.locator('[data-testid="deploys-table"]')).toBeVisible();
    await expect(deploysBtn(page)).toHaveClass(/active/);
    await expect(deployRows(page, 'wip')).toHaveCount(0);
  });
});
