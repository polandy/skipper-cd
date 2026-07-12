import { test, expect } from '../fixtures/test';
import type { Page } from '@playwright/test';

// Maske B: Logs-View. See docs/e2e-tests.md §4.3.

// UB1 — View toggle. The deploys↔logs toggle switches which pane is visible and
// persists the choice in `localStorage.activeView`, so a reload restores the last
// view. Asserting the panes' visibility (not the button's active class) proves the
// toggle actually swaps the rendered surface, and the reloads prove persistence.
test.describe('UB1: view toggle', () => {
  const deploysBtn = (page: Page) =>
    page.locator('[data-testid="view-toggle"] button[data-view="deploys"]');
  const logsBtn = (page: Page) =>
    page.locator('[data-testid="view-toggle"] button[data-view="logs"]');
  const deployTable = (page: Page) => page.locator('#deploy-table');
  const logView = (page: Page) => page.locator('#log-view');

  test('switches deploys↔logs and persists across reload', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);

    // Default view is deploys: the (populated) table shows, the log pane is hidden.
    await expect(deployTable(page)).toBeVisible();
    await expect(logView(page)).toBeHidden();

    // Switch to logs: the pane appears, the table is hidden, choice persisted.
    await logsBtn(page).click();
    await expect(logView(page)).toBeVisible();
    await expect(deployTable(page)).toBeHidden();
    expect(await page.evaluate(() => localStorage.getItem('activeView'))).toBe('logs');

    // A reload restores the logs view from localStorage.
    await page.reload();
    await expect(logView(page)).toBeVisible();
    await expect(deployTable(page)).toBeHidden();

    // Switch back to deploys: the table returns, the pane hides, choice persisted.
    await deploysBtn(page).click();
    await expect(deployTable(page)).toBeVisible();
    await expect(logView(page)).toBeHidden();
    expect(await page.evaluate(() => localStorage.getItem('activeView'))).toBe('deploys');

    // And a reload restores deploys.
    await page.reload();
    await expect(deployTable(page)).toBeVisible();
    await expect(logView(page)).toBeHidden();
  });
});

// UB2 — Log lines + level badges. Real backend log output is replayed over the
// /api/logs SSE stream and rendered as `log-line`s, each carrying its slog level
// as `data-level` plus a matching `level-badge`. A failing startup deploy emits
// INFO ("deploying stack") and ERROR ("docker compose up failed…") lines, and a
// webhook with a bad signature adds a WARN ("webhook rejected: invalid
// signature") — a deterministic INFO/WARN/ERROR mix produced by the real backend.
// DEBUG is intentionally out of scope: the default slog handler filters below
// INFO and skipper exposes no log-level toggle, so a DEBUG line can never reach
// the ring through the real binary.
test.describe('UB2: log lines + level badges', () => {
  test.use({
    startOptions: {
      stacks: ['web'],
      stubEnv: { STUB_DOCKER_FAIL_ON: 'up' },
      readiness: 'listening',
    },
  });

  const lineAtLevel = (page: Page, level: string) =>
    page.locator(`[data-testid="log-line"][data-level="${level}"]`);

  test('renders replayed lines with the correct level badge per level', async ({ page, skipper }) => {
    // The rejected webhook yields the WARN line; INFO + ERROR come from the
    // failed startup deploy already captured in the ring.
    expect(await skipper.sendBadWebhook('refs/heads/main')).toBe(401);

    await page.goto(`${skipper.baseURL}/`);
    await page.locator('[data-testid="view-toggle"] button[data-view="logs"]').click();

    // Each level renders at least one line whose badge text equals the level —
    // proving the level → badge mapping end-to-end against real log output.
    for (const level of ['INFO', 'WARN', 'ERROR']) {
      const line = lineAtLevel(page, level).first();
      await expect(line).toBeVisible();
      await expect(line.locator('[data-testid="level-badge"]')).toHaveText(level);
    }
  });
});

// UB3 — Sort toggle. The log pane defaults to newest-first (descending); the
// `log-sort` toggle flips the rendered order to oldest-first and persists the
// choice in `localStorage.logSort`, so a reload keeps the chosen order. We
// fingerprint the rendered line order (the DOM sequence of every `log-line`)
// and assert the toggle reverses it exactly, then that a reload preserves it —
// selecting lines only by `data-testid` and treating localStorage as the
// persisted source of truth rather than probing the button's active class.
test.describe('UB3: sort toggle', () => {
  test.use({
    startOptions: {
      stacks: ['web'],
      stubEnv: { STUB_DOCKER_FAIL_ON: 'up' },
      readiness: 'listening',
    },
  });

  const sortBtn = (page: Page) => page.locator('[data-testid="log-sort"]');
  const logLines = (page: Page) => page.locator('[data-testid="log-line"]');
  const lineOrder = (page: Page) => logLines(page).allTextContents();
  const logSort = (page: Page) =>
    page.evaluate(() => localStorage.getItem('logSort'));

  test('flips newest↔oldest order and persists across reload', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);
    await page.locator('[data-testid="view-toggle"] button[data-view="logs"]').click();

    // Wait for the replayed backlog to settle (the terminal ERROR line is last).
    await expect(page.locator('[data-testid="log-line"][data-level="ERROR"]').first()).toBeVisible();
    const lineCount = await logLines(page).count();
    expect(lineCount).toBeGreaterThan(1);

    // Default order is newest-first, and no explicit choice is stored yet.
    expect(await logSort(page)).toBeNull();
    const descOrder = await lineOrder(page);

    // Toggling flips to oldest-first: the rendered order is the exact reverse,
    // and the choice is persisted as ascending.
    await sortBtn(page).click();
    await expect(logLines(page)).toHaveCount(lineCount);
    const ascOrder = await lineOrder(page);
    expect(ascOrder).toEqual([...descOrder].reverse());
    expect(await logSort(page)).toBe('asc');

    // A reload restores the ascending order from localStorage.
    await page.reload();
    await page.locator('[data-testid="view-toggle"] button[data-view="logs"]').click();
    await expect(logLines(page)).toHaveCount(lineCount);
    expect(await lineOrder(page)).toEqual(ascOrder);
    expect(await logSort(page)).toBe('asc');

    // Toggling back returns to the original newest-first order.
    await sortBtn(page).click();
    await expect(logLines(page)).toHaveCount(lineCount);
    expect(await lineOrder(page)).toEqual(descOrder);
    expect(await logSort(page)).toBe('desc');
  });
});

// UB4 — Follow toggle. Following (the default) pins the pane to the newest edge
// whenever a fresh line streams in; unfollowing leaves the scroll position
// alone. The choice persists in `localStorage.followLogs`. We fill the ring so
// the pane overflows, scroll away from the newest edge (the top, in the default
// descending order), then drive one live line via a bad-signature webhook and
// assert the pane snaps back to `scrollTop === 0` only while following.
test.describe('UB4: follow toggle', () => {
  test.use({
    startOptions: {
      stacks: ['web'],
      stubEnv: { STUB_DOCKER_FAIL_ON: 'up' },
      readiness: 'listening',
    },
  });

  const logLines = (page: Page) => page.locator('[data-testid="log-line"]');
  const followBtn = (page: Page) => page.locator('[data-testid="follow-logs"]');
  const followLogs = (page: Page) =>
    page.evaluate(() => localStorage.getItem('followLogs'));
  const scrollTop = (page: Page) =>
    page.evaluate(() => document.getElementById('log-pane')!.scrollTop);
  // Scroll to the oldest edge (the bottom in descending order) so the newest
  // edge (top) is out of view — any autoscroll is then observable as scrollTop→0.
  const scrollAwayFromNewest = (page: Page) =>
    page.evaluate(() => {
      const p = document.getElementById('log-pane')!;
      p.scrollTop = p.scrollHeight;
    });

  test('autoscrolls to the newest edge only while following, and persists', async ({ page, skipper }) => {
    // Fill the ring so the log pane overflows and becomes scrollable. Each
    // bad-signature webhook adds one live WARN line.
    for (let i = 0; i < 60; i++) {
      expect(await skipper.sendBadWebhook('refs/heads/main')).toBe(401);
    }

    await page.goto(`${skipper.baseURL}/`);
    await page.locator('[data-testid="view-toggle"] button[data-view="logs"]').click();
    await expect(logLines(page).first()).toBeVisible();

    // Precondition: the pane actually overflows, so scrolling is meaningful.
    const overflows = await page.evaluate(() => {
      const p = document.getElementById('log-pane')!;
      return p.scrollHeight > p.clientHeight;
    });
    expect(overflows).toBe(true);

    // Following is on by default (no explicit choice stored yet).
    expect(await followLogs(page)).toBeNull();

    // Scroll away from the newest edge, then a fresh line streams in: following
    // snaps the pane back to the newest edge (scrollTop 0 in descending order).
    await scrollAwayFromNewest(page);
    expect(await scrollTop(page)).toBeGreaterThan(0);
    let count = await logLines(page).count();
    expect(await skipper.sendBadWebhook('refs/heads/main')).toBe(401);
    await expect(logLines(page)).toHaveCount(count + 1);
    expect(await scrollTop(page)).toBe(0);

    // Turn following off; the choice is persisted.
    await followBtn(page).click();
    expect(await followLogs(page)).toBe('false');

    // Scroll away again: now a fresh line must NOT drag the pane to the newest
    // edge — the reading position is left where the user put it.
    await scrollAwayFromNewest(page);
    expect(await scrollTop(page)).toBeGreaterThan(0);
    count = await logLines(page).count();
    expect(await skipper.sendBadWebhook('refs/heads/main')).toBe(401);
    await expect(logLines(page)).toHaveCount(count + 1);
    expect(await scrollTop(page)).toBeGreaterThan(0);

    // The unfollowed choice survives a reload.
    await page.reload();
    await page.locator('[data-testid="view-toggle"] button[data-view="logs"]').click();
    expect(await followLogs(page)).toBe('false');
  });
});
