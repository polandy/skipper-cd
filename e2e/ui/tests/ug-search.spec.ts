import { test, expect } from '../fixtures/test';
import type { Page } from '@playwright/test';

// Maske G: Deploys stack search. See dev-docs/e2e-tests.md §4.8.
//
// Three stacks deploy on startup (config order web → api → db), giving three
// success rows to filter. The filter is purely client-side; nothing is faked
// because the rows come from the real startup deploys.

test.use({ startOptions: { stacks: ['web', 'api', 'db'] } });

const rows = (page: Page) => page.locator('[data-testid="deploy-row"]');
const visibleRows = (page: Page) => page.locator('[data-testid="deploy-row"]:visible');
const row = (page: Page, stack: string) =>
  page.locator(`[data-testid="deploy-row"][data-stack="${stack}"]`);
const wrap = (page: Page) => page.locator('[data-testid="deploy-filter-wrap"]');
const input = (page: Page) => page.locator('[data-testid="deploy-filter"]');
const emptyNote = (page: Page) => page.locator('[data-testid="deploy-filter-empty"]');
const searchRow = (page: Page) => page.locator('[data-testid="deploy-search"]');
const deploysBtn = (page: Page) =>
  page.locator('[data-testid="view-toggle"] button[data-view="deploys"]');

// UG1 — Desktop type-to-search. The filter bar is hidden until the user types;
// a printable key reveals it, seeds the first character, and filters the rows by
// stack name (non-matching rows hidden). Esc clears the field, then folds the
// bar away.
test('UG1: typing reveals the filter, narrows the rows, and Esc folds it away', async ({
  page,
  skipper,
}) => {
  await page.goto(`${skipper.baseURL}/`);
  await expect(rows(page)).toHaveCount(3); // three startup success rows

  // Nothing typed yet: the bar is collapsed (its container has zero height).
  await expect(wrap(page)).toBeHidden();

  // Type "api": the bar reveals, the field is focused and carries the seed, and
  // only the matching stack's row stays visible.
  await page.keyboard.type('api');
  await expect(wrap(page)).toBeVisible();
  await expect(input(page)).toBeFocused();
  await expect(input(page)).toHaveValue('api');
  await expect(row(page, 'api')).toBeVisible();
  await expect(row(page, 'web')).toBeHidden();
  await expect(row(page, 'db')).toBeHidden();

  // First Esc clears the query — every row is back — but keeps the bar open.
  await page.keyboard.press('Escape');
  await expect(input(page)).toHaveValue('');
  await expect(visibleRows(page)).toHaveCount(3);
  await expect(wrap(page)).toBeVisible();

  // Second Esc (empty field) folds the bar away again.
  await page.keyboard.press('Escape');
  await expect(wrap(page)).toBeHidden();
});

// UG2 — No match. A query that matches no stack hides every row and shows the
// "No stack matches" note echoing the query.
test('UG2: a non-matching query shows the empty note', async ({ page, skipper }) => {
  await page.goto(`${skipper.baseURL}/`);
  await expect(rows(page)).toHaveCount(3);

  await page.keyboard.type('zzz');
  await expect(visibleRows(page)).toHaveCount(0);
  await expect(emptyNote(page)).toBeVisible();
  await expect(emptyNote(page)).toContainText('zzz');

  // Clearing restores every row and hides the note.
  await input(page).fill('');
  await expect(visibleRows(page)).toHaveCount(3);
  await expect(emptyNote(page)).toBeHidden();
});

// UG3 — Mobile entry point. Touch has no keyboard to trigger type-to-search, so
// on a narrow viewport the deploys view-options popover carries a "Search stacks"
// row that reveals and focuses the filter. That row is desktop-hidden.
test('UG3: the mobile popover "Search stacks" row reveals and focuses the filter', async ({
  page,
  skipper,
}) => {
  await page.goto(`${skipper.baseURL}/`);
  await expect(rows(page)).toHaveCount(3);

  // Desktop: opening the deploys popover shows the view options, but NOT the
  // search row (type-to-search covers it there).
  await deploysBtn(page).click();
  await expect(page.locator('[data-testid="view-options"]')).toHaveClass(/\bopen\b/);
  await expect(searchRow(page)).toBeHidden();
  await page.keyboard.press('Escape'); // close the popover
  await expect(page.locator('[data-testid="view-options"]')).not.toHaveClass(/\bopen\b/);

  // Narrow viewport (phone): the search row appears in the popover.
  await page.setViewportSize({ width: 390, height: 800 });
  await expect(wrap(page)).toBeHidden();
  await deploysBtn(page).click();
  await expect(searchRow(page)).toBeVisible();

  // Tapping it closes the popover, reveals the bar, and focuses the field.
  await searchRow(page).click();
  await expect(page.locator('[data-testid="view-options"]')).not.toHaveClass(/\bopen\b/);
  await expect(wrap(page)).toBeVisible();
  await expect(input(page)).toBeFocused();

  // Typing then filters exactly as on desktop.
  await page.keyboard.type('db');
  await expect(row(page, 'db')).toBeVisible();
  await expect(row(page, 'web')).toBeHidden();
  await expect(row(page, 'api')).toBeHidden();
});
