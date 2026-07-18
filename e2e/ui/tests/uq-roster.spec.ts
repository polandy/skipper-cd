import { test, expect } from '../fixtures/test';
import type { Page } from '@playwright/test';

// Maske Q: Stacks roster view (dev-docs/stack-roster-spec.md). See
// dev-docs/e2e-tests.md §4.18.
//
// A third top-level view listing the full stack set skipper owns (stack
// discovery, ADR-0034) with each stack's last outcome — inventory, not an event
// log. Covers the tab switch + deployed/disabled rows + discovery hint,
// click-a-row-for-history (reusing the deploy table's audit panel), and the
// search filter (mirrors the deploys filter, incl. the mobile popover entry).
// The "never deployed" synthetic state is unit-tested (internal/roster) and
// shares its .roster-flag rendering with the disabled row asserted here.
// Behaviour-only (no snapshot).

const stacksBtn = (page: Page) => page.locator('[data-testid="view-toggle"] button[data-view="stacks"]');
const rows = (page: Page) => page.locator('[data-testid="roster-row"]');
const visibleRows = (page: Page) => page.locator('[data-testid="roster-row"]:visible');
const row = (page: Page, stack: string) => page.locator(`[data-testid="roster-row"][data-stack="${stack}"]`);
const wrap = (page: Page) => page.locator('[data-testid="roster-filter-wrap"]');
const input = (page: Page) => page.locator('[data-testid="roster-filter"]');
const emptyNote = (page: Page) => page.locator('[data-testid="roster-filter-empty"]');
const searchRow = (page: Page) => page.locator('[data-testid="roster-search"]');

test.use({
  startOptions: {
    stacks: ['api', 'web', 'wip'],
    discovery: { repoConfig: 'stacks:\n  wip:\n    disabled: true\n', disabled: ['wip'] },
  },
});

// UQ1 — the roster lists the whole set with last outcome + discovery hint, and
// the view replaces the deploy table.
test('UQ1: lists the full set with last outcome and discovery hint', async ({ page, skipper }) => {
  await page.goto(`${skipper.baseURL}/`);
  await stacksBtn(page).click();

  await expect(page.locator('[data-testid="stacks-view"]')).toBeVisible();
  await expect(page.locator('[data-testid="deploys-table"]')).toBeHidden();

  // Every declared stack has a row — enabled (deployed) first alphabetically,
  // then the parked one: api, web, wip.
  await expect(rows(page)).toHaveCount(3);
  await expect(rows(page).nth(0)).toHaveAttribute('data-stack', 'api');
  await expect(rows(page).nth(1)).toHaveAttribute('data-stack', 'web');
  await expect(rows(page).nth(2)).toHaveAttribute('data-stack', 'wip');

  // Deployed stacks carry a real status badge; the parked one is muted with the
  // disabled flag and no badge.
  await expect(row(page, 'api').locator('[data-testid="status-badge"]')).toHaveText('success');
  await expect(row(page, 'wip')).toHaveClass(/disabled/);
  await expect(row(page, 'wip').locator('.roster-flag')).toHaveText('disabled');
  await expect(row(page, 'wip').locator('[data-testid="status-badge"]')).toHaveCount(0);

  await expect(page.locator('[data-testid="roster-count"]')).toHaveText('3 stacks');
  await expect(page.locator('[data-testid="roster-source"]')).toHaveText('discovery');

  // The view is a top-level peer: back to deploys restores the table.
  await page.locator('[data-testid="view-toggle"] button[data-view="deploys"]').click();
  await expect(page.locator('[data-testid="stacks-view"]')).toBeHidden();
  await expect(page.locator('[data-testid="deploys-table"]')).toBeVisible();
});

// UQ2 — clicking a row toggles its deploy-history panel; one open at a time.
test('UQ2: clicking a row toggles its deploy-history panel', async ({ page, skipper }) => {
  await page.goto(`${skipper.baseURL}/`);
  await stacksBtn(page).click();

  const apiRow = row(page, 'api');
  await apiRow.click();
  const panel = page.locator('[data-testid="audit-panel"]');
  await expect(panel).toHaveCount(1);
  await expect(panel).toHaveAttribute('data-audit-for', 'api');
  await expect(panel.locator('[data-testid="audit-row"]').first()).toBeVisible();
  await expect(apiRow).toHaveClass(/audit-open/);

  // Clicking the same row again closes it.
  await apiRow.click();
  await expect(panel).toHaveCount(0);

  // Opening another row's history closes the first — one panel at a time.
  await apiRow.click();
  await expect(panel).toHaveCount(1);
  await row(page, 'web').click();
  await expect(page.locator('[data-testid="audit-panel"]')).toHaveCount(1);
  await expect(page.locator('[data-testid="audit-panel"]')).toHaveAttribute('data-audit-for', 'web');
});

// UQ3 — desktop type-to-search: a printable key reveals the bar, seeds it, and
// filters by stack name; the empty note echoes a no-match query.
test('UQ3: typing filters the roster and shows the empty note on no match', async ({ page, skipper }) => {
  await page.goto(`${skipper.baseURL}/`);
  await stacksBtn(page).click();
  await expect(rows(page)).toHaveCount(3);
  await expect(wrap(page)).toBeHidden();

  await page.keyboard.type('api');
  await expect(wrap(page)).toBeVisible();
  await expect(input(page)).toBeFocused();
  await expect(input(page)).toHaveValue('api');
  await expect(row(page, 'api')).toBeVisible();
  await expect(row(page, 'web')).toBeHidden();
  await expect(row(page, 'wip')).toBeHidden();
  await expect(page.locator('[data-testid="roster-filter-count"]')).toHaveText('1/3');

  // No match: every row hidden, the note echoes the query.
  await input(page).fill('zzz');
  await expect(visibleRows(page)).toHaveCount(0);
  await expect(emptyNote(page)).toBeVisible();
  await expect(emptyNote(page)).toContainText('zzz');

  // First Esc clears, second folds the bar away.
  await page.keyboard.press('Escape');
  await expect(input(page)).toHaveValue('');
  await expect(visibleRows(page)).toHaveCount(3);
  await expect(wrap(page)).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(wrap(page)).toBeHidden();
});

// UQ4 — mobile entry point: touch has no keyboard, so the stacks view-options
// popover carries a desktop-hidden "Search stacks" row that reveals the filter.
test('UQ4: the mobile popover "Search stacks" row reveals and focuses the filter', async ({ page, skipper }) => {
  await page.goto(`${skipper.baseURL}/`);
  await stacksBtn(page).click();
  await expect(rows(page)).toHaveCount(3);

  // Desktop: the search row is hidden (type-to-search covers it).
  await expect(searchRow(page)).toBeHidden();

  // Narrow viewport: reopen the popover via the active Stacks button; the search
  // row now shows and reveals + focuses the filter when tapped.
  await page.setViewportSize({ width: 390, height: 800 });
  await stacksBtn(page).click();
  await expect(page.locator('[data-testid="view-options"]')).toHaveClass(/\bopen\b/);
  await expect(searchRow(page)).toBeVisible();
  await searchRow(page).click();
  await expect(page.locator('[data-testid="view-options"]')).not.toHaveClass(/\bopen\b/);
  await expect(wrap(page)).toBeVisible();
  await expect(input(page)).toBeFocused();

  await page.keyboard.type('wip');
  await expect(row(page, 'wip')).toBeVisible();
  await expect(row(page, 'api')).toBeHidden();
  await expect(row(page, 'web')).toBeHidden();
});
