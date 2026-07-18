import { test, expect } from '../fixtures/test';
import type { Page, Locator } from '@playwright/test';

// Maske S: cross-view stack jump. See dev-docs/e2e-tests.md §4.20.
//
// The compass jump-btn beside every stack name (deploy row and roster row
// alike) switches between the Deploys and Stacks views and lands on that
// stack's row there — Deploys -> Stacks always lands on the one roster row
// (inventory); Stacks -> Deploys lands on the *newest* deploy row (the table
// is an event log, newest first). Behaviour-only (no new snapshot — the
// deploy-table/theme/mobile baselines already regenerated for the jump-btn's
// footprint on every row).

const deploysBtn = (page: Page) => page.locator('[data-testid="view-toggle"] button[data-view="deploys"]');
const stacksBtn = (page: Page) => page.locator('[data-testid="view-toggle"] button[data-view="stacks"]');
const deployRows = (page: Page, stack: string) =>
  page.locator(`[data-testid="deploy-row"][data-stack="${stack}"]`);
const rosterRow = (page: Page, stack: string) =>
  page.locator(`[data-testid="roster-row"][data-stack="${stack}"]`);
const jumpBtnIn = (row: Locator) => row.locator('[data-testid="jump-btn"]');

test.describe('cross-view jump', () => {
  test.use({ startOptions: { stacks: ['api', 'web'] } });

  // US1 — a deploy row's jump button switches to Stacks and lands on that
  // stack's roster row (flashed via .jump-target, cleared again after).
  test('US1: Deploys -> Stacks jump lands on and flashes the roster row', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);

    await jumpBtnIn(deployRows(page, 'api').first()).click();

    await expect(page.locator('[data-testid="stacks-view"]')).toBeVisible();
    await expect(page.locator('[data-testid="deploys-table"]')).toBeHidden();
    await expect(stacksBtn(page)).toHaveClass(/active/);

    const target = rosterRow(page, 'api');
    await expect(target).toBeVisible();
    await expect(target).toHaveClass(/jump-target/);
    // The flash is temporary, not a persisted state like audit-open.
    await expect(target).not.toHaveClass(/jump-target/, { timeout: 3000 });
  });

  // US2 — a roster row's jump button switches to Deploys and lands on the
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
    await jumpBtnIn(rosterRow(page, 'api')).click();

    await expect(page.locator('[data-testid="deploys-table"]')).toBeVisible();
    await expect(deploysBtn(page)).toHaveClass(/active/);
    await expect(deployRows(page, 'api').first()).toHaveClass(/jump-target/);
    await expect(deployRows(page, 'api').nth(1)).not.toHaveClass(/jump-target/);
  });

  // US3 — the jump button pre-empts the row's own click handler: it must
  // switch view instead of also opening the row's diff/history panel (a
  // regression guard: both listeners are on the same delegated element).
  test('US3: the jump button does not also open the row/panel it sits on', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);

    await jumpBtnIn(deployRows(page, 'api').first()).click();
    await expect(page.locator('[data-testid="stacks-view"]')).toBeVisible();
    // No diff/files panel opened on the deploy row behind the jump.
    await expect(deployRows(page, 'api').first()).not.toHaveClass(/diff-open/);

    await jumpBtnIn(rosterRow(page, 'api')).click();
    await expect(page.locator('[data-testid="deploys-table"]')).toBeVisible();
    // No history/containers panel opened on the roster row behind the jump.
    await expect(rosterRow(page, 'api')).not.toHaveClass(/audit-open/);
    await expect(page.locator('[data-testid="audit-panel"]')).toHaveCount(0);
  });
});

// US4 — a stack the Deploys view has no row for yet (parked, never deployed)
// still carries a jump button on its roster row; the jump degrades to a plain
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

    await jumpBtnIn(rosterRow(page, 'wip')).click();

    await expect(page.locator('[data-testid="deploys-table"]')).toBeVisible();
    await expect(deploysBtn(page)).toHaveClass(/active/);
    await expect(deployRows(page, 'wip')).toHaveCount(0);
  });
});
