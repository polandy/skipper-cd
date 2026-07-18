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
    const row = page.locator('[data-testid="deploy-row"][data-stack="web"]');
    await expect(p).toHaveAttribute('data-health', 'unhealthy');

    await p.click();
    const panel = page.locator('[data-testid="health-panel"]');
    await expect(panel).toBeVisible();
    await expect(panel.locator('[data-testid="health-service"]')).toHaveCount(2);
    // The panel is bound to its row (variant A): the row is marked open + coloured.
    await expect(row).toHaveClass(/health-open/);
    await expect(row).toHaveAttribute('data-health', 'unhealthy');

    await p.click();
    await expect(panel).toHaveCount(0);
    await expect(row).not.toHaveClass(/health-open/);
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

// UH5 — on-demand containers (ADR-0027 amendment): skipper stops a stack's
// on_demand_containers itself after the deploy (often exit 137), so an exited
// one classifies as stopped — the stack stays healthy — and the panel labels
// the service on-demand instead of letting the exit look like a failure.
test.describe('UH5: on-demand container reads stopped, not unhealthy', () => {
  test.use({
    startOptions: {
      stacks: ['web'],
      healthPoll: 1,
      onDemand: { web: ['web-app'] },
      initialHealth: {
        web: [
          { Service: 'app', Name: 'web-app', State: 'exited', ExitCode: 137 },
          { Service: 'db', Name: 'web-db', State: 'running', Health: 'healthy' },
        ],
      },
    },
  });

  test('the stack rolls up healthy and the panel labels the idle service', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);

    // The killed-but-intended-idle app must not degrade the stack.
    const p = pill(page, 'web');
    await expect(p).toHaveAttribute('data-health', 'healthy');

    await p.click();
    const services = page.locator('[data-testid="health-service"]');
    await expect(services).toHaveCount(2);
    const app = services.filter({ hasText: 'app' }).first();
    await expect(app.locator('.hp-status')).toHaveAttribute('data-health', 'stopped');
    await expect(app.locator('.hp-state')).toHaveText(/exited · on-demand/);
    // The sibling keeps its plain state text.
    await expect(services.filter({ hasText: 'db' }).first().locator('.hp-state')).toHaveText('running');
  });
});

// UH4 — status history (ADR-0031): with the health watch on, the per-service
// panel shows the current phase's age, and — once a service has more than one
// accepted phase — a timeline of its phases, with the deploy's short commit as
// a chip on a phase that began right after a deploy. A lone baseline shows no
// timeline (it would only repeat the inline age).
test.describe('UH4: status history (health watch)', () => {
  test.use({
    startOptions: {
      stacks: ['web'],
      healthPoll: 1,
      healthWatch: true,
      initialHealth: { web: [{ Service: 'app', State: 'running', Health: 'healthy' }] },
    },
  });

  test('a transition grows a timeline with a deploy-correlated commit chip', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);
    const p = pill(page, 'web');
    await expect(p).toHaveAttribute('data-health', 'healthy');

    // Baseline only: the service line carries the inline age, but no timeline —
    // a single phase would just repeat it.
    await p.click();
    const panel = page.locator('[data-testid="health-panel"]');
    await expect(panel.locator('.hp-for')).toHaveCount(1);
    await expect(panel.locator('[data-testid="health-history"]')).toHaveCount(0);
    await p.click();

    // A deploy records the commit context, then the service turns unhealthy
    // within the attribution window: the accepted phase is deploy-correlated.
    skipper.setStackImage('web', '1.26');
    expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);
    const rows = page.locator('[data-testid="deploy-row"][data-stack="web"]');
    await expect(rows).toHaveCount(2);
    await expect(rows.first().locator('[data-testid="status-badge"]')).toHaveText('success');

    skipper.setStackHealth('web', [{ Service: 'app', State: 'running', Health: 'unhealthy' }]);
    const newestPill = rows.first().locator('[data-testid="health-pill"]');
    await expect(newestPill).toHaveAttribute('data-health', 'unhealthy');

    // Reopen: two accepted phases now — the timeline appears, newest first,
    // with the commit chip on the correlated unhealthy phase only.
    await newestPill.click();
    const history = panel.locator('[data-testid="health-history"]');
    await expect(history).toHaveCount(1);
    const phases = history.locator('[data-testid="health-phase"]');
    await expect(phases).toHaveCount(2);
    await expect(phases.first()).toHaveAttribute('data-health', 'unhealthy');
    await expect(phases.nth(1)).toHaveAttribute('data-health', 'healthy');
    const chip = phases.first().locator('[data-testid="health-phase-commit"]');
    await expect(chip).toHaveText(/^[0-9a-f]{7}$/);
    await expect(phases.nth(1).locator('[data-testid="health-phase-commit"]')).toHaveCount(0);
  });
});

// UH6 — the pill is a real button: keyboard users can reach it with Tab and
// toggle the per-service panel with Enter/Space, like the files pill and the
// history button (which are native <button>s too).
test.describe('UH6: health pill is keyboard-operable', () => {
  test.use({ startOptions: { stacks: ['web'], healthPoll: 1 } });

  test('the pill takes focus and Enter toggles the panel', async ({ page, skipper }) => {
    skipper.setStackHealth('web', [{ Service: 'app', State: 'running', Health: 'healthy' }]);
    await page.goto(`${skipper.baseURL}/`);

    const p = pill(page, 'web');
    await expect(p).toHaveAttribute('data-health', 'healthy');

    await p.focus();
    await expect(p).toBeFocused(); // a plain <span> would refuse focus

    await page.keyboard.press('Enter');
    await expect(page.locator('[data-testid="health-panel"]')).toHaveCount(1);

    await page.keyboard.press('Enter'); // still focused: toggles closed again
    await expect(page.locator('[data-testid="health-panel"]')).toHaveCount(0);
  });
});
