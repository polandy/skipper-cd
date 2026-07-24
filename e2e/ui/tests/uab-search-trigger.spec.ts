import { test, expect } from '../fixtures/test';
import type { Page } from '@playwright/test';

// Maske AB: Always-visible search trigger (T3.11). See dev-docs/e2e-tests.md §4.29.
//
// Before this, the deploys/stacks stack filter was desktop-invisible — only
// type-to-search revealed it, so the primary filter was an easter egg. A quiet
// magnifier now sits in the header (left of the view switch) and opens the same
// bar with a click. It is view-aware (deploys → deploy filter, stacks → roster
// filter), hidden on the Logs view (which has its own in-panel search), and
// reflects the open state via .active + aria-expanded. Type-to-search still works.

test.use({ startOptions: { stacks: ['web', 'api', 'db'] } });

const searchBtn = (page: Page) => page.locator('[data-testid="stack-search-btn"]');
const deployWrap = (page: Page) => page.locator('[data-testid="deploy-filter-wrap"]');
const deployInput = (page: Page) => page.locator('[data-testid="deploy-filter"]');
const rosterWrap = (page: Page) => page.locator('[data-testid="roster-filter-wrap"]');
const rosterInput = (page: Page) => page.locator('[data-testid="roster-filter"]');
const viewBtn = (page: Page, view: string) =>
  page.locator(`[data-testid="view-toggle"] button[data-view="${view}"]`);

// UAB1 — Deploys: the magnifier is always visible (the fix), and clicking it
// reveals the filter, focuses the input, and marks itself open; a second click
// folds the bar away again.
test('UAB1: the header magnifier opens and closes the deploys filter', async ({
  page,
  skipper,
}) => {
  await page.goto(`${skipper.baseURL}/`);
  await expect(page.locator('[data-testid="deploy-row"]')).toHaveCount(3);

  // Always visible on desktop deploys — no typing needed to discover it.
  await expect(searchBtn(page)).toBeVisible();
  await expect(searchBtn(page)).toHaveAttribute('aria-expanded', 'false');
  await expect(deployWrap(page)).toBeHidden();

  // Click reveals the bar, focuses the field, and marks the trigger open.
  await searchBtn(page).click();
  await expect(deployWrap(page)).toBeVisible();
  await expect(deployInput(page)).toBeFocused();
  await expect(searchBtn(page)).toHaveClass(/\bactive\b/);
  await expect(searchBtn(page)).toHaveAttribute('aria-expanded', 'true');

  // The revealed bar filters exactly like type-to-search.
  await page.keyboard.type('api');
  await expect(page.locator('[data-testid="deploy-row"][data-stack="api"]')).toBeVisible();
  await expect(page.locator('[data-testid="deploy-row"][data-stack="web"]')).toBeHidden();

  // Second click folds the bar away and clears the open state.
  await searchBtn(page).click();
  await expect(deployWrap(page)).toBeHidden();
  await expect(searchBtn(page)).not.toHaveClass(/\bactive\b/);
  await expect(searchBtn(page)).toHaveAttribute('aria-expanded', 'false');
});

// UAB2 — Stacks: the same magnifier opens the roster filter on the stacks view.
test('UAB2: the magnifier opens the stacks (roster) filter', async ({ page, skipper }) => {
  await page.goto(`${skipper.baseURL}/`);
  await viewBtn(page, 'stacks').click();
  await expect(page.locator('[data-testid="roster-row"]')).not.toHaveCount(0);

  await expect(searchBtn(page)).toBeVisible();
  await expect(rosterWrap(page)).toBeHidden();

  await searchBtn(page).click();
  await expect(rosterWrap(page)).toBeVisible();
  await expect(rosterInput(page)).toBeFocused();
  await expect(searchBtn(page)).toHaveClass(/\bactive\b/);
});

// UAB3 — Logs: the header magnifier is hidden; log search lives in the log panel.
test('UAB3: the magnifier is hidden on the Logs view', async ({ page, skipper }) => {
  await page.goto(`${skipper.baseURL}/`);
  await expect(searchBtn(page)).toBeVisible(); // shown on deploys
  await viewBtn(page, 'logs').click();
  await expect(searchBtn(page)).toBeHidden();
  // Back on deploys it returns.
  await viewBtn(page, 'deploys').click();
  await expect(searchBtn(page)).toBeVisible();
});

// UAB4 — Type-to-search still works, and the magnifier reflects a bar opened by
// typing (not only by clicking the trigger) — the two entry points stay in sync.
test('UAB4: type-to-search still reveals the bar and the magnifier shows open', async ({
  page,
  skipper,
}) => {
  await page.goto(`${skipper.baseURL}/`);
  await expect(page.locator('[data-testid="deploy-row"]')).toHaveCount(3);

  await expect(deployWrap(page)).toBeHidden();
  await page.keyboard.type('db');
  await expect(deployWrap(page)).toBeVisible();
  await expect(deployInput(page)).toHaveValue('db');
  // The trigger reflects the bar that typing opened.
  await expect(searchBtn(page)).toHaveClass(/\bactive\b/);
  await expect(searchBtn(page)).toHaveAttribute('aria-expanded', 'true');

  // Esc folds it away and the trigger returns to closed.
  await page.keyboard.press('Escape'); // clears "db"
  await page.keyboard.press('Escape'); // folds away
  await expect(deployWrap(page)).toBeHidden();
  await expect(searchBtn(page)).not.toHaveClass(/\bactive\b/);
});
