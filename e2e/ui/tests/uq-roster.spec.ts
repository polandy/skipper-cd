import { test, expect } from '../fixtures/test';
import type { Page } from '@playwright/test';

// Maske Q: Stacks roster view (dev-docs/stack-roster-spec.md, shared design in
// dev-docs/ui-design-concept.md). See dev-docs/e2e-tests.md §4.18.
//
// A third top-level view listing the full stack set skipper owns (stack
// discovery, ADR-0034) with each stack's last outcome — inventory, not an event
// log, rendered as an aligned table that reuses the deploy table's row/column/
// expand language. Covers the view switch + deployed/disabled rows + aligned
// column header (UQ1), click-a-row → containers + deploy history (UQ2), the
// search filter incl. the mobile popover entry (UQ3/UQ4), and the shared
// time-mode toggle (UQ5). The "never deployed" synthetic state is unit-tested
// (internal/roster) and shares its .roster-flag rendering with the disabled row
// asserted here. Behaviour-only (no snapshot).

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
    // Health on, so expanding a stack shows its containers panel (UQ2).
    healthPoll: 1,
    initialHealth: {
      api: [{ Service: 'api', State: 'running', Health: 'healthy' }],
      web: [{ Service: 'web', State: 'running', Health: 'healthy' }],
    },
  },
});

// UQ1 — the roster lists the whole set as an aligned table (column header, no
// count/title line) and the view replaces the deploy table.
test('UQ1: lists the full set as an aligned table and replaces the deploy table', async ({ page, skipper }) => {
  await page.goto(`${skipper.baseURL}/`);
  await stacksBtn(page).click();

  await expect(page.locator('[data-testid="stacks-view"]')).toBeVisible();
  await expect(page.locator('[data-testid="deploys-table"]')).toBeHidden();

  // Aligned column header, like the deploy table — and no count/mode line above.
  const header = page.locator('.roster-list-header');
  await expect(header).toBeVisible();
  await expect(header).toContainText('Stack');
  await expect(header).toContainText('Last deploy');
  await expect(header).toContainText('Commit');

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

  // The view is a top-level peer: back to deploys restores the table.
  await page.locator('[data-testid="view-toggle"] button[data-view="deploys"]').click();
  await expect(page.locator('[data-testid="stacks-view"]')).toBeHidden();
  await expect(page.locator('[data-testid="deploys-table"]')).toBeVisible();
});

// UQ2 — clicking a row expands the stack into its containers (health) panel
// above its deploy-history panel; one stack open at a time.
test('UQ2: clicking a row shows the stack containers and deploy history', async ({ page, skipper }) => {
  await page.goto(`${skipper.baseURL}/`);
  // Wait until the health snapshot has landed (the deploy table shows a pill),
  // so the containers panel is populated once we expand a roster row.
  await expect(page.locator('[data-testid="health-pill"]').first()).toBeVisible();
  await stacksBtn(page).click();

  const apiRow = row(page, 'api');
  await apiRow.click();
  await expect(apiRow).toHaveClass(/audit-open/);

  const health = page.locator('[data-testid="health-panel"]');
  const audit = page.locator('[data-testid="audit-panel"]');
  // Containers panel first (from the health snapshot), then the history panel.
  await expect(health).toHaveCount(1);
  await expect(health.locator('[data-testid="health-service"]')).toHaveCount(1);
  await expect(audit).toHaveCount(1);
  await expect(audit).toHaveAttribute('data-audit-for', 'api');
  await expect(audit.locator('[data-testid="audit-row"]').first()).toBeVisible();

  // Clicking the same row again closes both panels.
  await apiRow.click();
  await expect(health).toHaveCount(0);
  await expect(audit).toHaveCount(0);

  // Opening another row closes the first — one stack at a time.
  await apiRow.click();
  await row(page, 'web').click();
  await expect(page.locator('[data-testid="audit-panel"]')).toHaveCount(1);
  await expect(page.locator('[data-testid="audit-panel"]')).toHaveAttribute('data-audit-for', 'web');
  await expect(page.locator('[data-testid="health-panel"]')).toHaveCount(1);
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

// UQ5 — the shared time-mode toggle in the stacks popover switches the roster's
// times from relative to absolute (one mode, shared with the deploys toggle).
test('UQ5: the time-mode toggle switches roster times to absolute', async ({ page, skipper }) => {
  await page.goto(`${skipper.baseURL}/`);
  await stacksBtn(page).click();

  const when = row(page, 'api').locator('.roster-when');
  // Relative by default ("just now" / "Ns ago") — never a clock time.
  await expect(when).not.toContainText(':');

  // Reopen the popover via the active Stacks button and flip to absolute.
  await stacksBtn(page).click();
  const toggle = page.locator('[data-testid="roster-time-mode"]');
  await toggle.click();
  await expect(toggle).toHaveClass(/active/);
  await expect(when).toContainText(':'); // absolute now shows a clock time
});
