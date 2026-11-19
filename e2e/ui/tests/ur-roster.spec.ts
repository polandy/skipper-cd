import { test, expect } from '../fixtures/test';
import type { Page } from '@playwright/test';

// Maske R: Stacks roster view (docs/dev/stack-roster-spec.md, shared design in
// docs/dev/ui-design-concept.md). See docs/dev/e2e-tests.md §4.19.
//
// A third top-level view listing the full stack set skipper owns (stack
// discovery, ADR-0034) with each stack's last outcome — inventory, not an event
// log, rendered as an aligned table that reuses the deploy table's row/column/
// expand language. Covers the view switch + deployed/disabled rows + aligned
// column header (UR1), click-a-row → the detail panel's containers + history
// tabs (UR2; the panel itself is Maske BC, §4.56), the
// search filter incl. the mobile popover entry (UR3/UR4), and the shared
// time-mode toggle (UR5). The "never deployed" synthetic state is unit-tested
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
    // Health on, so expanding a stack shows its containers panel (UR2).
    healthPoll: 1,
    initialHealth: {
      api: [{ Service: 'api', State: 'running', Health: 'healthy' }],
      web: [{ Service: 'web', State: 'running', Health: 'healthy' }],
    },
  },
});

// UR1 — the roster lists the whole set as an aligned table (column header, no
// count/title line) and the view replaces the deploy table.
test('UR1: lists the full set as an aligned table and replaces the deploy table', async ({ page, skipper }) => {
  await page.goto(`${skipper.baseURL}/`);
  await stacksBtn(page).click();

  await expect(page.locator('[data-testid="stacks-view"]')).toBeVisible();
  await expect(page.locator('[data-testid="deploys-table"]')).toBeHidden();

  // Aligned column header, like the deploy table — and no count/mode line above.
  const header = page.locator('.roster-list-header');
  await expect(header).toBeVisible();
  await expect(header.locator('> span')).toHaveText(['Stack', 'Health', 'Version', 'Last deploy']);

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

// UR2 — clicking a row opens the stack's detail panel on its containers tab (a
// healthy, deployed stack), and the history tab shows its deploy history; one
// stack open at a time.
test('UR2: clicking a row shows the stack containers and deploy history', async ({ page, skipper }) => {
  await page.goto(`${skipper.baseURL}/`);
  // Wait until the health snapshot has landed (the roster shows a pill), so
  // the containers tab is offered once we open a roster row.
  await stacksBtn(page).click();
  await expect(page.locator('[data-testid="roster-row"] [data-testid="health-pill"]').first()).toBeVisible();

  const apiRow = row(page, 'api');
  await apiRow.click();
  await expect(apiRow).toHaveClass(/audit-open/);

  const panel = page.locator('[data-testid="detail-panel"]');
  await expect(panel).toHaveCount(1);
  await expect(panel).toHaveAttribute('data-stack', 'api');
  await expect(panel).toHaveAttribute('data-tab', 'containers');
  const health = panel.locator('[data-testid="health-panel"]');
  await expect(health.locator('[data-testid="health-service"]')).toHaveCount(1);

  await panel.locator('[data-testid="detail-tab"][data-tab="history"]').click();
  const audit = panel.locator('[data-testid="audit-panel"]');
  await expect(audit).toHaveAttribute('data-audit-for', 'api');
  await expect(audit.locator('[data-testid="audit-row"]').first()).toBeVisible();
  await expect(health).toHaveCount(0);

  // Clicking the same row again closes the panel.
  await apiRow.click();
  await expect(panel).toHaveCount(0);

  // Opening another row closes the first — one stack at a time.
  await apiRow.click();
  await row(page, 'web').click();
  await expect(panel).toHaveCount(1);
  await expect(panel).toHaveAttribute('data-stack', 'web');
  await expect(apiRow).not.toHaveClass(/audit-open/);
});

// UR3 — desktop type-to-search: a printable key reveals the bar, seeds it, and
// filters by stack name; the empty note echoes a no-match query.
test('UR3: typing filters the roster and shows the empty note on no match', async ({ page, skipper }) => {
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

// UR4 — mobile entry point: touch has no keyboard, so the stacks view-options
// popover carries a desktop-hidden "Search stacks" row that reveals the filter.
test('UR4: the mobile popover "Search stacks" row reveals and focuses the filter', async ({ page, skipper }) => {
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

// UR5 — the shared time-mode toggle in the stacks popover switches the roster's
// times from relative to absolute (one mode, shared with the deploys toggle).
test('UR5: the time-mode toggle switches roster times to absolute', async ({ page, skipper }) => {
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

// UR6 — an open panel survives a roster republish. The `stacks` snapshot lands
// after every run, and a rebuild that drops the open panel yanks the answer away
// from whoever is reading it — precisely when the panel has something new to
// say. The panel is re-opened from the new snapshot instead, which is also the
// seam that makes the change-detection lead (Maske AJ) deterministic: it can
// only ever be read after the republish, never before it.
test('UR6: an open panel survives a roster republish and carries the new data', async ({ page, skipper }) => {
  await page.goto(`${skipper.baseURL}/`);
  // Let the startup deploy settle before pushing, so the change below gets its
  // own run (and its own commit) rather than being swept up by the one already
  // in flight. Row counts are the seam; nothing here waits on the clock.
  const deployed = page.locator('[data-testid="deploy-row"][data-stack="api"][data-status="success"]');
  await expect(deployed).toHaveCount(1);
  await stacksBtn(page).click();
  await expect(page.locator('[data-testid="roster-row"] [data-testid="health-pill"]').first()).toBeVisible();

  const apiRow = row(page, 'api');
  await apiRow.click();
  const panel = page.locator('[data-testid="detail-panel"]');
  await panel.locator('[data-testid="detail-tab"][data-tab="triggers"]').click();
  const watched = page.locator('[data-testid="watched-panel"]');
  await expect(watched).toHaveCount(1);
  // The startup deploy has no prior commit to diff against, so the lead names
  // the deploy itself rather than a SHA.
  await expect(watched.locator('[data-testid="watched-lead"]')).toContainText('Unchanged since the last deploy.');

  // A deploy republishes the roster underneath the open panel.
  skipper.setStackImage('api', '1.26');
  expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);

  // Still open, on the same row and the same tab — and now carrying what the
  // run produced: the commit its inputs have not changed since.
  await expect(watched.locator('[data-testid="watched-lead"]')).toContainText(/^Unchanged since [0-9a-f]{7}\./);
  await expect(watched).toHaveCount(1);
  await expect(apiRow).toHaveClass(/audit-open/);
  await expect(panel).toHaveAttribute('data-stack', 'api');
  await expect(panel).toHaveAttribute('data-tab', 'triggers');
});
