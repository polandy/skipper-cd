import { test, expect } from '../fixtures/test';

// Maske H: Stack health (ADR-0027). See dev-docs/e2e-tests.md §4.9.
//
// The health poller is enabled with `healthPoll: 1` (poll every second) and each
// stack's `docker compose ps --format json` output is scripted via the stub
// (skipper.setStackHealth), so these are deterministic and offline: no real
// docker, no real containers.

const pill = (page: import('@playwright/test').Page, stack: string) =>
  page.locator(`[data-testid="deploy-row"][data-stack="${stack}"] [data-testid="health-pill"]`);

// UH1 — a health snapshot renders a pill per stack with the rolled-up status:
// running/healthy → healthy, any unhealthy → unhealthy, exited(0) → stopped.
test.describe('UH1: health pill per stack', () => {
  test.use({ startOptions: { stacks: ['web', 'db', 'cache'], healthPoll: 1 } });

  test('renders the rolled-up status as a coloured pill on each stack', async ({ page, skipper }) => {
    skipper.setStackHealth('web', [{ Service: 'app', State: 'running', Health: 'healthy' }]);
    skipper.setStackHealth('db', [{ Service: 'app', State: 'running', Health: 'unhealthy' }]);
    skipper.setStackHealth('cache', [{ Service: 'app', State: 'exited', ExitCode: 0 }]);
    await page.goto(`${skipper.baseURL}/`);

    await expect(pill(page, 'web')).toHaveAttribute('data-health', 'healthy');
    await expect(pill(page, 'db')).toHaveAttribute('data-health', 'unhealthy');
    await expect(pill(page, 'cache')).toHaveAttribute('data-health', 'stopped');
  });
});

// UH2 — clicking the pill toggles a per-service breakdown panel below the row.
// The stack rolls up to unhealthy (one service unhealthy) while the panel lists
// every service.
test.describe('UH2: per-service panel', () => {
  test.use({ startOptions: { stacks: ['web'], healthPoll: 1 } });

  test('clicking the pill opens the per-service panel and closes it again', async ({ page, skipper }) => {
    skipper.setStackHealth('web', [
      { Service: 'app', State: 'running', Health: 'healthy' },
      { Service: 'db', State: 'running', Health: 'unhealthy' },
    ]);
    await page.goto(`${skipper.baseURL}/`);

    const p = pill(page, 'web');
    await expect(p).toHaveAttribute('data-health', 'unhealthy');

    await p.click();
    const panel = page.locator('[data-testid="health-panel"]');
    await expect(panel).toBeVisible();
    await expect(panel.locator('[data-testid="health-service"]')).toHaveCount(2);

    await p.click();
    await expect(panel).toHaveCount(0);
  });
});

// UH3 — health is a current per-stack value, so the pill sits only on the newest
// row of each stack and moves to a freshly-deployed row.
test.describe('UH3: newest row per stack', () => {
  test.use({ startOptions: { stacks: ['web'], healthPoll: 1 } });

  test('the pill stays on the newest row when a new deploy prepends a row', async ({ page, skipper }) => {
    skipper.setStackHealth('web', [{ Service: 'app', State: 'running', Health: 'healthy' }]);
    await page.goto(`${skipper.baseURL}/`);

    const rows = page.locator('[data-testid="deploy-row"][data-stack="web"]');
    const pills = page.locator('[data-testid="deploy-row"][data-stack="web"] [data-testid="health-pill"]');
    await expect(rows).toHaveCount(1);
    await expect(pills).toHaveCount(1);

    // A pushed change prepends a second row for the same stack.
    skipper.setStackImage('web', '1.26');
    expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);
    await expect(rows).toHaveCount(2);

    // Exactly one pill, on the newest (first) row — not the older one.
    await expect(pills).toHaveCount(1);
    await expect(rows.first().locator('[data-testid="health-pill"]')).toHaveCount(1);
    await expect(rows.nth(1).locator('[data-testid="health-pill"]')).toHaveCount(0);
  });
});
