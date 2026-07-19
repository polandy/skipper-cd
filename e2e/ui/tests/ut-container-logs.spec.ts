import { test, expect } from '../fixtures/test';
import type { Page } from '@playwright/test';

// Maske T: Container logs (ADR-0037). See dev-docs/e2e-tests.md §4.21.
//
// The health poller (healthPoll: 1) surfaces the per-container log icons on the
// health panel's service lines and validates the {service} segment. The stub
// docker answers `compose … logs` with a fixed backlog (see harness.ts): a
// single service drops the compose prefix (--no-log-prefix), the whole stack
// keeps it (`<stack>-1  | `). No real docker, no real containers.

const rowLogBtn = (page: Page, stack: string) =>
  page.locator(`[data-testid="deploy-row"][data-stack="${stack}"] [data-testid="clog-btn"]`);

// UT1 — the row icon streams the whole stack's logs (services merged, prefixed).
test.describe('UT1: per-stack log panel', () => {
  test.use({ startOptions: { stacks: ['web'], healthPoll: 1 } });

  test('opens a live panel from the row icon and streams the backlog', async ({ page, skipper }) => {
    skipper.setStackHealth('web', [{ Service: 'app', State: 'running', Health: 'healthy' }]);
    await page.goto(`${skipper.baseURL}/`);

    const btn = rowLogBtn(page, 'web');
    await btn.click();

    const panel = page.locator('[data-testid="clog-panel"]');
    await expect(panel).toBeVisible();
    await expect(panel.locator('[data-testid="clog-live"]')).toBeVisible();
    const body = panel.locator('[data-testid="clog-body"]');
    await expect(body).toContainText('listening on :8080');
    await expect(body).toContainText('web-1'); // merged view keeps the service prefix

    // Clicking the same icon closes it.
    await btn.click();
    await expect(panel).toHaveCount(0);
  });
});

// UT2 — each health-panel service line opens that one container's logs.
test.describe('UT2: per-container log panel', () => {
  test.use({ startOptions: { stacks: ['web'], healthPoll: 1 } });

  test('a service line opens its single-service log', async ({ page, skipper }) => {
    skipper.setStackHealth('web', [
      { Service: 'app', State: 'running', Health: 'healthy' },
      { Service: 'db', State: 'running', Health: 'healthy' },
    ]);
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
  test.use({ startOptions: { stacks: ['web', 'db'], healthPoll: 1 } });

  test('opening a second log closes the first', async ({ page, skipper }) => {
    skipper.setStackHealth('web', [{ Service: 'app', State: 'running', Health: 'healthy' }]);
    skipper.setStackHealth('db', [{ Service: 'app', State: 'running', Health: 'healthy' }]);
    await page.goto(`${skipper.baseURL}/`);

    await rowLogBtn(page, 'web').click();
    await expect(page.locator('[data-testid="clog-panel"]')).toHaveCount(1);
    await expect(page.locator('[data-testid="clog-panel"]')).toContainText('web');

    await rowLogBtn(page, 'db').click();
    await expect(page.locator('[data-testid="clog-panel"]')).toHaveCount(1);
    await expect(page.locator('[data-testid="clog-panel"]')).toContainText('db');
  });
});

// UT4 — typing while a log is open searches inside it (overriding the stack
// search): matching lines highlight, the rest hide, and a hit count shows.
test.describe('UT4: in-log type-to-search', () => {
  test.use({ startOptions: { stacks: ['web'], healthPoll: 1 } });

  test('typing filters the open log to matching lines', async ({ page, skipper }) => {
    skipper.setStackHealth('web', [{ Service: 'app', State: 'running', Health: 'healthy' }]);
    await page.goto(`${skipper.baseURL}/`);

    await rowLogBtn(page, 'web').click();
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

// UT5 — the Logs view carries search + wrap + fullscreen in the view-options
// popover like the other views; typing reveals its filter bar.
test.describe('UT5: Logs view controls in the popover', () => {
  test.use({ startOptions: { stacks: ['web'] } });

  test('type-to-search reveals the log filter; the popover carries the controls', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);
    await page.locator('[data-testid="view-toggle"] button[data-view="logs"]').click();

    await expect(page.locator('[data-testid="log-search"]')).toHaveCount(1);
    await expect(page.locator('[data-testid="log-wrap"]')).toHaveCount(1);
    await expect(page.locator('[data-testid="log-fs"]')).toHaveCount(1);

    await page.keyboard.type('deploy', { delay: 25 });
    await expect(page.locator('[data-testid="log-filter-wrap"]')).toHaveClass(/revealed/);
    await expect(page.locator('[data-testid="log-filter"]')).toHaveValue('deploy');
  });
});

// UT6 — wrap and fullscreen are popover toggles that light when engaged.
test.describe('UT6: Logs view wrap + fullscreen toggles', () => {
  test.use({ startOptions: { stacks: ['web'] } });

  test('wrap and fullscreen toggle their active state', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);
    const logsBtn = page.locator('[data-testid="view-toggle"] button[data-view="logs"]');
    await logsBtn.click(); // switch to the Logs view (closes the popover)
    await logsBtn.click(); // active button reopens the options popover

    const wrap = page.locator('[data-testid="log-wrap"]');
    await wrap.click();
    await expect(wrap).toHaveClass(/active/);

    const fs = page.locator('[data-testid="log-fs"]');
    await fs.click();
    await expect(fs).toHaveClass(/active/);
  });
});
