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
