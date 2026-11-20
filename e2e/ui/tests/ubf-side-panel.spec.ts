import { test, expect } from '../fixtures/test';
import type { Locator, Page } from '@playwright/test';

// Maske BF: the detail panel beside the list on a wide screen (ADR-0073). See
// docs/dev/e2e-tests.md §4.59 and internal/ui/UI_SPEC.md "Detail placement".
//
// At ≥1200 px an open detail sits in the view's side column, right of a list
// narrowed to name + status (UBF1, UBF3); below that it stays inline under its
// row (UBF2); a resize moves the open detail between the two without closing it
// (UBF4); and the panel's own actions and Escape work from the side (UBF5).
// Behaviour-only (no snapshot).

const WIDE = { width: 1280, height: 800 };
const NARROW = { width: 1100, height: 800 };

const deployRow = (page: Page, stack: string) =>
  page.locator(`[data-testid="deploy-row"][data-stack="${stack}"]`).first();
const rosterRow = (page: Page, stack: string) =>
  page.locator(`[data-testid="roster-row"][data-stack="${stack}"]`);
const panel = (page: Page, stack: string) =>
  page.locator(`[data-testid="detail-panel"][data-stack="${stack}"]`);
const sidePanels = (page: Page, view: string) =>
  page.locator(`[data-testid="detail-side"][data-detail-view="${view}"] [data-testid="detail-panel"]`);

// besideList reports whether the panel's left edge lies right of the row's right edge.
async function besideList(page: Page, stack: string, row: Locator): Promise<boolean> {
  const p = await panel(page, stack).boundingBox();
  const r = await row.boundingBox();
  return !!p && !!r && p.x >= r.x + r.width;
}

async function nextSibling(row: Locator): Promise<string | null> {
  return row.evaluate((r) => r.nextElementSibling?.getAttribute('data-testid') ?? null);
}

test.describe('Maske BF: the detail panel beside the list', () => {
  test.use({ startOptions: { stacks: ['web', 'api'] } });

  // UBF1 — wide: the deploy detail opens in the side column, the list keeps
  // each row's name and status and drops its Change and When read-outs.
  test('UBF1: at 1280 px the deploy detail opens beside the list', async ({ page, skipper }) => {
    await page.setViewportSize(WIDE);
    await page.goto(`${skipper.baseURL}/`);
    const row = deployRow(page, 'web');
    await expect(row.locator('[data-testid="time-cell"]')).toBeVisible();

    await row.locator('.stack-name').click();
    await expect(sidePanels(page, 'deploys')).toHaveCount(1);
    expect(await besideList(page, 'web', row)).toBe(true);
    await expect(row.locator('.status-cell')).toBeVisible();
    await expect(row.locator('[data-testid="time-cell"]')).toBeHidden();

    await panel(page, 'web').locator('[data-testid="detail-close"]').click();
    await expect(panel(page, 'web')).toHaveCount(0);
    await expect(row.locator('[data-testid="time-cell"]')).toBeVisible();
  });

  // UBF2 — narrower: the detail stays inline right after its row.
  test('UBF2: at 1100 px the deploy detail opens under its row', async ({ page, skipper }) => {
    await page.setViewportSize(NARROW);
    await page.goto(`${skipper.baseURL}/`);
    const row = deployRow(page, 'web');
    await row.locator('.stack-name').click();
    await expect(panel(page, 'web')).toBeVisible();
    expect(await nextSibling(row)).toBe('detail-panel');
    await expect(sidePanels(page, 'deploys')).toHaveCount(0);
    await expect(row.locator('[data-testid="time-cell"]')).toBeVisible();
  });

  // UBF3 — the Stacks view follows the same placement.
  test('UBF3: at 1280 px the Stacks detail opens beside the roster', async ({ page, skipper }) => {
    await page.setViewportSize(WIDE);
    await page.goto(`${skipper.baseURL}/`);
    await page.locator('[data-testid="view-toggle"] button[data-view="stacks"]').click();
    const row = rosterRow(page, 'api');
    await row.click();
    await expect(sidePanels(page, 'stacks').and(panel(page, 'api'))).toHaveCount(1);
    expect(await besideList(page, 'api', row)).toBe(true);
    await expect(row.locator('.roster-status')).toBeVisible();
    await expect(row.locator('.roster-when')).toBeHidden();
    await expect(row.locator('[data-testid="roster-version"]')).toBeHidden();
  });

  // UBF4 — a resize across the breakpoint moves the open detail, never closes it.
  test('UBF4: a resize moves the open detail between side and inline', async ({ page, skipper }) => {
    await page.setViewportSize(WIDE);
    await page.goto(`${skipper.baseURL}/`);
    const row = deployRow(page, 'web');
    await row.locator('.stack-name').click();
    await expect(sidePanels(page, 'deploys')).toHaveCount(1);

    await page.setViewportSize(NARROW);
    await expect(sidePanels(page, 'deploys')).toHaveCount(0);
    expect(await nextSibling(row)).toBe('detail-panel');
    await expect(row).toHaveClass(/audit-open/);

    await page.setViewportSize(WIDE);
    await expect(sidePanels(page, 'deploys')).toHaveCount(1);
    await expect(row).toHaveClass(/audit-open/);
  });

  // UBF5 — the side panel keeps the inline one's controls: Escape inside it
  // closes it and hands the focus back to the row's toggle, and its Jump
  // action reaches the Stacks detail.
  test('UBF5: Jump and Escape work from the side panel', async ({ page, skipper }) => {
    await page.setViewportSize(WIDE);
    await page.goto(`${skipper.baseURL}/`);
    const row = deployRow(page, 'web');
    const toggle = row.locator('.row-toggle');
    await toggle.click();
    await panel(page, 'web').locator('[data-testid="detail-close"]').focus();
    await page.keyboard.press('Escape');
    await expect(panel(page, 'web')).toHaveCount(0);
    await expect(toggle).toBeFocused();

    await toggle.click();
    await panel(page, 'web').locator('[data-testid="jump-btn"]').click();
    await expect(page.locator('[data-testid="stacks-view"]')).toBeVisible();
    await expect(sidePanels(page, 'stacks').and(panel(page, 'web'))).toHaveCount(1);
  });
});
