import { test, expect } from '../fixtures/test';
import type { Locator, Page } from '@playwright/test';

// Maske BF: the detail panel beside the list on a wide screen (ADR-0073). See
// docs/dev/e2e-tests.md §4.59 and internal/ui/UI_SPEC.md "Detail placement".
//
// At ≥1200 px an open detail sits in the view's side column, right of a list
// narrowed to name + status (UBF1, UBF3); below that it stays inline under its
// row (UBF2); a resize moves the open detail between the two without closing it
// (UBF4), also while a filter hides its row (UBF6); and the panel's own actions
// and Escape work from the side (UBF5). On a phone the same column is a
// full-screen sheet over the list, and closing it returns to the list at its
// scroll position (UBF7, UBF8); a resize moves the sheet inline and back (UBF9).
// Behaviour-only (no snapshot).

const WIDE = { width: 1280, height: 800 };
const NARROW = { width: 1100, height: 800 };
const PHONE = { width: 390, height: 700 };
const MANY = Array.from({ length: 14 }, (_, i) => `s${String(i + 1).padStart(2, '0')}`);

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

  // UBF6 — a detail hidden inline with its filtered-out row crosses into the
  // side column hidden, and shows there once the filter clears.
  test('UBF6: a detail filtered out inline shows in the side column once the filter clears', async ({
    page,
    skipper,
  }) => {
    await page.setViewportSize(NARROW);
    await page.goto(`${skipper.baseURL}/`);
    await page.locator('[data-testid="view-toggle"] button[data-view="stacks"]').click();
    await rosterRow(page, 'api').click();
    await expect(panel(page, 'api')).toBeVisible();

    await page.locator('[data-testid="stack-search-btn"]').click();
    await page.locator('[data-testid="roster-filter"]').fill('web');
    await expect(panel(page, 'api')).toBeHidden();

    await page.setViewportSize(WIDE);
    await expect(sidePanels(page, 'stacks')).toHaveCount(1);
    await expect(panel(page, 'api')).toBeHidden();

    await page.locator('[data-testid="roster-filter"]').fill('');
    await expect(panel(page, 'api')).toBeVisible();
    expect(await besideList(page, 'api', rosterRow(page, 'api'))).toBe(true);
  });
});

// coversViewport reports whether the panel's sheet spans the whole viewport and
// sits above the header there.
async function coversViewport(page: Page, stack: string): Promise<boolean> {
  return page.evaluate((sel) => {
    const p = document.querySelector(sel);
    const sheet = p?.closest('[data-testid="detail-side"]');
    if (!sheet) return false;
    const b = sheet.getBoundingClientRect();
    const top = document.elementFromPoint(innerWidth / 2, 10);
    return b.x === 0 && b.y === 0 && b.width === innerWidth && b.height === innerHeight && sheet.contains(top);
  }, `[data-testid="detail-panel"][data-stack="${stack}"]`);
}

test.describe('Maske BF: the detail panel as a phone sheet', () => {
  test.use({ startOptions: { stacks: MANY } });

  // UBF7 — the Stacks detail opens as a sheet over the scrolled roster; the
  // page under it keeps its scroll, and closing lands on the same spot.
  test('UBF7: on a phone the Stacks detail is a sheet and close keeps the scroll', async ({ page, skipper }) => {
    await page.setViewportSize(PHONE);
    await page.goto(`${skipper.baseURL}/`);
    await page.locator('[data-testid="view-toggle"] button[data-view="stacks"]').click();
    const row = rosterRow(page, 's12');
    await row.scrollIntoViewIfNeeded();
    const scrolled = await page.evaluate(() => scrollY);
    expect(scrolled).toBeGreaterThan(0);

    await row.click();
    await expect(sidePanels(page, 'stacks').and(panel(page, 's12'))).toHaveCount(1);
    expect(await coversViewport(page, 's12')).toBe(true);
    await page.mouse.wheel(0, 2000);
    await page.mouse.wheel(0, -4000);
    expect(await page.evaluate(() => scrollY)).toBe(scrolled);

    await panel(page, 's12').locator('[data-testid="detail-close"]').click();
    await expect(panel(page, 's12')).toHaveCount(0);
    await expect(row).toBeInViewport();
    expect(await page.evaluate(() => scrollY)).toBe(scrolled);
  });

  // UBF8 — the Deploys detail takes the same sheet, which takes the focus off
  // the covered row; Escape closes it and hands the focus back to the toggle.
  test('UBF8: on a phone the deploy sheet takes the focus and closes on Escape', async ({ page, skipper }) => {
    await page.setViewportSize(PHONE);
    await page.goto(`${skipper.baseURL}/`);
    const row = deployRow(page, 's01');
    const toggle = row.locator('.row-toggle');
    await toggle.click();
    await expect(sidePanels(page, 'deploys')).toHaveCount(1);
    expect(await coversViewport(page, 's01')).toBe(true);
    await expect(panel(page, 's01')).toBeFocused();

    await page.keyboard.press('Escape');
    await expect(panel(page, 's01')).toHaveCount(0);
    await expect(toggle).toBeFocused();
  });

  // UBF9 — widening past the phone breakpoint moves the sheet under its row,
  // narrowing again turns it back into the sheet, without closing it.
  test('UBF9: a resize moves the open detail between sheet and inline', async ({ page, skipper }) => {
    await page.setViewportSize(PHONE);
    await page.goto(`${skipper.baseURL}/`);
    const row = deployRow(page, 's01');
    await row.locator('.stack-name').click();
    await expect(sidePanels(page, 'deploys')).toHaveCount(1);

    await page.setViewportSize(NARROW);
    await expect(sidePanels(page, 'deploys')).toHaveCount(0);
    expect(await nextSibling(row)).toBe('detail-panel');

    await page.setViewportSize(PHONE);
    await expect(sidePanels(page, 'deploys')).toHaveCount(1);
    expect(await coversViewport(page, 's01')).toBe(true);
  });
});
