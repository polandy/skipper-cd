import { test, expect } from '../fixtures/test';
import { openRowMenu } from '../fixtures/menu';
import type { Page } from '@playwright/test';

// Maske T: Container logs (ADR-0037). See dev-docs/e2e-tests.md §4.21.
//
// The health poller surfaces the per-container log icons on the health panel's
// service lines and validates the {service} segment. The stub docker answers
// `compose … logs` with a fixed backlog (see harness.ts): a single service drops
// the compose prefix (--no-log-prefix), the whole stack keeps it (`<stack>-1  | `).
// No real docker, no real containers.
//
// Health is seeded via `initialHealth` (written before skipper starts), not
// setStackHealth in the body: the startup deploy's first health poll would
// otherwise run before the body writes the stub file, caching a `stopped` /
// no-services snapshot that the client reads on connect — leaving the panel empty
// ("No services reported") and no clog-btn to click. That boot race was UT2's flake.

const APP_DB = [
  { Service: 'app', State: 'running', Health: 'healthy' },
  { Service: 'db', State: 'running', Health: 'healthy' },
];
const APP_ONLY = [{ Service: 'app', State: 'running', Health: 'healthy' }];

const deployRow = (page: Page, stack: string) =>
  page.locator(`[data-testid="deploy-row"][data-stack="${stack}"]`);
// The per-stack log icon lives inside the newest row's ⋯ overflow menu (T3.13),
// so opening a row log means: open the menu, then click its Container-logs item.
const rowLogBtn = (page: Page, stack: string) =>
  deployRow(page, stack).locator('[data-testid="clog-btn"]');
const openRowLog = async (page: Page, stack: string) => {
  await openRowMenu(deployRow(page, stack));
  await rowLogBtn(page, stack).click();
};

// UT1 — the row icon streams the whole stack's logs (services merged, prefixed).
test.describe('UT1: per-stack log panel', () => {
  test.use({ startOptions: { stacks: ['web'], healthPoll: 1, initialHealth: { web: APP_ONLY } } });

  test('opens a live panel from the row icon and streams the backlog', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);

    await openRowLog(page, 'web');

    const panel = page.locator('[data-testid="clog-panel"]');
    await expect(panel).toBeVisible();
    await expect(panel.locator('[data-testid="clog-live"]')).toBeVisible();
    const body = panel.locator('[data-testid="clog-body"]');
    await expect(body).toContainText('listening on :8080');
    await expect(body).toContainText('web-1'); // merged view keeps the service prefix

    // Clicking the same icon closes it (reopen the menu to reach it again).
    await openRowLog(page, 'web');
    await expect(panel).toHaveCount(0);
  });
});

// UT2 — each health-panel service line opens that one container's logs.
test.describe('UT2: per-container log panel', () => {
  test.use({ startOptions: { stacks: ['web'], healthPoll: 1, initialHealth: { web: APP_DB } } });

  test('a service line opens its single-service log', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);

    await page.locator('[data-testid="deploy-row"][data-stack="web"] [data-testid="health-pill"]').click();
    const svc = page.locator('[data-testid="health-panel"] [data-testid="health-service"]').first();
    await svc.locator('[data-testid="clog-btn"]').click();

    const panel = page.locator('[data-testid="clog-panel"]');
    await expect(panel).toBeVisible();
    await expect(panel).toContainText('web / app'); // scope names the single service
    const body = panel.locator('[data-testid="clog-body"]');
    await expect(body).toContainText('listening on :8080');
    await expect(body).not.toContainText('| 2026-01-01'); // single service → no compose prefix
  });
});

// UT3 — only one log is open at a time; a new one replaces the old.
test.describe('UT3: single log open', () => {
  test.use({
    startOptions: { stacks: ['web', 'db'], healthPoll: 1, initialHealth: { web: APP_ONLY, db: APP_ONLY } },
  });

  test('opening a second log closes the first', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);

    await openRowLog(page, 'web');
    await expect(page.locator('[data-testid="clog-panel"]')).toHaveCount(1);
    await expect(page.locator('[data-testid="clog-panel"]')).toContainText('web');

    await openRowLog(page, 'db');
    await expect(page.locator('[data-testid="clog-panel"]')).toHaveCount(1);
    await expect(page.locator('[data-testid="clog-panel"]')).toContainText('db');
  });
});

// UT4 — typing while a log is open searches inside it (overriding the stack
// search): matching lines highlight, the rest hide, and a hit count shows.
test.describe('UT4: in-log type-to-search', () => {
  test.use({ startOptions: { stacks: ['web'], healthPoll: 1, initialHealth: { web: APP_ONLY } } });

  test('typing filters the open log to matching lines', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);

    await openRowLog(page, 'web');
    const body = page.locator('[data-testid="clog-body"]');
    await expect(body.locator('.clog-ln')).not.toHaveCount(0);

    await page.keyboard.type('ERROR', { delay: 25 });

    // The in-log search box opened and took the query (not the stack filter).
    await expect(page.locator('[data-testid="clog-search"] input')).toHaveValue('ERROR');
    await expect(page.locator('[data-testid="deploy-filter-wrap"]')).not.toHaveClass(/revealed/);

    // Exactly the ERROR backlog line matches; it stays highlighted and visible.
    const hit = body.locator('.clog-ln.clog-hit');
    await expect(hit).toHaveCount(1);
    await expect(hit).toContainText('ERROR upstream timeout');
    await expect(page.locator('[data-testid="clog-panel"] .clog-hits')).toContainText('1 hit');
  });
});

// UT5/UT6 (the Logs view's own search/wrap/fullscreen/live controls) moved to
// ub-logs.spec.ts as UB8/UB9 once the view became a page-sized clog-panel with
// those controls inline in its own header instead of a popover.
