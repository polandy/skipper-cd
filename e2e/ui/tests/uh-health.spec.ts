import { test, expect } from '../fixtures/test';
import { openStackDetail } from '../fixtures/stack-detail';

// Maske H: Stack health (ADR-0027). See docs/dev/e2e-tests.md §4.9.
//
// The health poller is enabled with `healthPoll: 1` (poll every second) and each
// stack's `docker compose ps --format json` output is scripted via the stub
// (skipper.setStackHealth), so these are deterministic and offline: no real
// docker, no real containers. Health is a current per-stack value, so it lives
// on the Stacks view: the roster row's pill and the detail's Containers tab.

const pill = (page: import('@playwright/test').Page, stack: string) =>
  page.locator(`[data-testid="roster-row"][data-stack="${stack}"] [data-testid="health-pill"]`);
const gotoStacks = (page: import('@playwright/test').Page) =>
  page.locator('[data-testid="view-toggle"] button[data-view="stacks"]').click();
const openContainers = (page: import('@playwright/test').Page) =>
  openStackDetail(page, 'web', 'containers');
const closeDetail = (page: import('@playwright/test').Page) =>
  page.locator('[data-testid="detail-close"]').click();

// UH1 — a health snapshot renders a pill per stack with the rolled-up status:
// running/healthy → healthy, any unhealthy → unhealthy, exited(0) → stopped.
test.describe('UH1: health pill per stack', () => {
  test.use({ startOptions: { stacks: ['web', 'db', 'cache'], healthPoll: 1 } });

  test('renders the rolled-up status as a coloured pill on each stack', async ({ page, skipper }) => {
    skipper.setStackHealth('web', [{ Service: 'app', State: 'running', Health: 'healthy' }]);
    skipper.setStackHealth('db', [{ Service: 'app', State: 'running', Health: 'unhealthy' }]);
    skipper.setStackHealth('cache', [{ Service: 'app', State: 'exited', ExitCode: 0 }]);
    await page.goto(`${skipper.baseURL}/`);
    await gotoStacks(page);

    await expect(pill(page, 'web')).toHaveAttribute('data-health', 'healthy');
    await expect(pill(page, 'db')).toHaveAttribute('data-health', 'unhealthy');
    await expect(pill(page, 'cache')).toHaveAttribute('data-health', 'stopped');
  });
});

// UH2 — the Containers tab of the stack's detail breaks the roll-up down per
// service: the stack rolls up to unhealthy (one service unhealthy) while the
// panel lists every service.
test.describe('UH2: per-service panel', () => {
  test.use({ startOptions: { stacks: ['web'], healthPoll: 1 } });

  test('the Containers tab lists every service and closes with the detail', async ({ page, skipper }) => {
    skipper.setStackHealth('web', [
      { Service: 'app', State: 'running', Health: 'healthy' },
      { Service: 'db', State: 'running', Health: 'unhealthy' },
    ]);
    await page.goto(`${skipper.baseURL}/`);
    await gotoStacks(page);
    await expect(pill(page, 'web')).toHaveAttribute('data-health', 'unhealthy');

    await openContainers(page);
    const panel = page.locator('[data-testid="detail-panel"] [data-testid="health-panel"]');
    await expect(panel).toBeVisible();
    await expect(panel.locator('[data-testid="health-service"]')).toHaveCount(2);

    await closeDetail(page);
    await expect(panel).toHaveCount(0);
  });
});

// UH3 — a deploy row is history, not current state: it carries no health pill,
// however many rows a stack has, while the Stacks row shows the live roll-up.
test.describe('UH3: deploy rows carry no health pill', () => {
  test.use({ startOptions: { stacks: ['web'], healthPoll: 1 } });

  test('the pill stays on the Stacks row when a deploy prepends a row', async ({ page, skipper }) => {
    skipper.setStackHealth('web', [{ Service: 'app', State: 'running', Health: 'healthy' }]);
    await page.goto(`${skipper.baseURL}/`);

    const rows = page.locator('[data-testid="deploy-row"][data-stack="web"]');
    skipper.setStackImage('web', '1.26');
    expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);
    await expect(rows).toHaveCount(2);
    await expect(rows.first()).toHaveAttribute('data-status', 'success');

    // The roll-up arrived (positive signal), yet no deploy row renders it.
    await gotoStacks(page);
    await expect(pill(page, 'web')).toHaveAttribute('data-health', 'healthy');
    await expect(page.locator('[data-testid="deploy-row"] [data-testid="health-pill"]')).toHaveCount(0);
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
    await gotoStacks(page);
    await expect(pill(page, 'web')).toHaveAttribute('data-health', 'healthy');

    await openContainers(page);
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
    await gotoStacks(page);
    const p = pill(page, 'web');
    await expect(p).toHaveAttribute('data-health', 'healthy');

    // Baseline only: the service line carries the inline age, but no timeline —
    // a single phase would just repeat it.
    await openContainers(page);
    const panel = page.locator('[data-testid="health-panel"]');
    await expect(panel.locator('.hp-for')).toHaveCount(1);
    await expect(panel.locator('[data-testid="health-history"]')).toHaveCount(0);
    await closeDetail(page);

    // A deploy records the commit context, then the service turns unhealthy
    // within the attribution window: the accepted phase is deploy-correlated.
    skipper.setStackImage('web', '1.26');
    expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);
    const rows = page.locator('[data-testid="deploy-row"][data-stack="web"]');
    await expect(rows).toHaveCount(2);
    await expect(rows.first().locator('[data-testid="status-badge"]')).toHaveText('success');

    skipper.setStackHealth('web', [{ Service: 'app', State: 'running', Health: 'unhealthy' }]);
    await expect(p).toHaveAttribute('data-health', 'unhealthy');

    // Reopen: two accepted phases now — the timeline appears, newest first,
    // with the commit chip on the correlated unhealthy phase only.
    await openContainers(page);
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

// UH7 — the timeline folds routine churn (UI_SPEC "Status history"): a short
// starting → healthy cycle is absorbed into the settled line as `· up in Xs`
// instead of standing as its own phase line, the strip summarizes the phases
// as a bar, and the raw newest-first list stays reachable behind the toggle.
test.describe('UH7: routine start folds away, raw list behind the toggle', () => {
  test.use({
    startOptions: {
      stacks: ['web'],
      healthPoll: 1,
      healthWatch: true,
      initialHealth: { web: [{ Service: 'app', State: 'running', Health: 'healthy' }] },
    },
  });

  test('a restart cycle folds into "up in"; the toggle swaps in all phases', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);
    await gotoStacks(page);
    const p = pill(page, 'web');
    await expect(p).toHaveAttribute('data-health', 'healthy');

    // A routine restart: healthy → starting → healthy, each flip accepted
    // immediately (debounce_polls 1). Wait for each accepted phase via the
    // pill, so the history deterministically holds three phases.
    skipper.setStackHealth('web', [{ Service: 'app', State: 'running', Health: 'starting' }]);
    await expect(p).toHaveAttribute('data-health', 'starting');
    skipper.setStackHealth('web', [{ Service: 'app', State: 'running', Health: 'healthy' }]);
    await expect(p).toHaveAttribute('data-health', 'healthy');

    await openContainers(page);
    const panel = page.locator('[data-testid="health-panel"]');
    const history = panel.locator('[data-testid="health-history"]');
    await expect(history).toBeVisible();

    // Folded: the starting phase is no line of its own — it rides the current
    // healthy line as "up in Xs". Two lines (current + baseline), not three.
    const phases = history.locator('[data-testid="health-phase"]');
    await expect(phases).toHaveCount(2);
    await expect(phases.first()).toHaveAttribute('data-health', 'healthy');
    await expect(phases.first().locator('.hp-upin')).toHaveText(/up in \d+s/);

    // The strip carries one duration-weighted segment per phase.
    await expect(history.locator('[data-testid="health-strip"] span')).toHaveCount(3);

    // The toggle swaps in the verbatim list (3 phases, starting in the middle)
    // and back.
    const toggle = history.locator('[data-testid="health-fold-toggle"]');
    await expect(toggle).toHaveText('all 3 phases');
    // It names the list it reveals, so the id the panel mints actually resolves.
    const controls = await toggle.getAttribute('aria-controls');
    expect(controls).toBeTruthy();
    await expect(history.locator(`#${controls}`)).toHaveClass(/hp-raw/);
    await toggle.click();
    const raw = history.locator('.hp-raw .hp-phase');
    await expect(raw).toHaveCount(3);
    await expect(raw.nth(1)).toHaveAttribute('data-health', 'starting');
    await expect(phases.first()).toBeHidden(); // folded view is swapped out
    await toggle.click();
    await expect(phases.first()).toBeVisible();
    await expect(raw.first()).toBeHidden();
  });
});
