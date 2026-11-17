import { test, expect } from '../fixtures/test';

// Maske N: Self-heal row detail — the drifted-services panel (ADR-0029
// amendment). See docs/dev/e2e-tests.md §4.15.
//
// Builds on Maske K: drives the *real* self-heal loop (health poller reports the
// stack degraded → self-heal redeploys → the stack recovers → a `healed` row
// appears), then asserts
// the row's detail. A heal is not a git deploy, so the healed row's Change cell
// stays empty and a click opens the deploy detail on its Drift tab: the verdict
// explains the corrective redeploy, the tab lists the services that had drifted
// when it ran. Behaviour-only (no snapshot).

const healthyBoth = [
  { Service: 'app', State: 'running', Health: 'healthy' },
  { Service: 'db', State: 'running', Health: 'healthy' },
];
// app degraded, db still healthy: the rollup is unhealthy (so self-heal fires),
// but only `app` should show up as drifted — the healthy service is filtered.
const appDegraded = [
  { Service: 'app', State: 'running', Health: 'unhealthy' },
  { Service: 'db', State: 'running', Health: 'healthy' },
];

const healedRow = (page: import('@playwright/test').Page) =>
  page
    .locator('[data-testid="deploy-row"][data-stack="web"][data-status="healed"]')
    .first();

test.describe('UN1: a healed row opens the drift detail', () => {
  test.use({
    startOptions: {
      stacks: ['web'],
      healthPoll: 1,
      selfHeal: true,
      selfHealMinUnhealthyPolls: 1,
      selfHealCooldownSeconds: 1,
      selfHealMaxAttempts: 5,
      initialHealth: { web: healthyBoth },
    },
  });

  test('the row opens the detail explaining the heal and listing the drifted service', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);
    await expect(
      page.locator('[data-testid="deploy-row"][data-stack="web"][data-status="success"]'),
    ).toHaveCount(1);

    // Degrade the stack → self-heal runs a corrective redeploy → the stack
    // recovers → a healed row (a heal is reported only once it took).
    const upsBefore = skipper.dockerUps('web');
    skipper.setStackHealth('web', appDegraded);
    await expect(() => expect(skipper.dockerUps('web')).toBeGreaterThan(upsBefore)).toPass();
    skipper.setStackHealth('web', healthyBoth);
    const row = healedRow(page);
    await expect(row).toBeVisible();

    // A heal applied no change: the Change cell is empty.
    await expect(row.locator('.col-version')).toHaveText('');

    // Click the row → the deploy detail opens below it on its Drift tab.
    const opener = row.locator('.stack-name');
    await opener.click();
    const panel = page.locator('[data-testid="detail-panel"][data-kind="deploy"]');
    await expect(panel).toBeVisible();
    await expect(panel).toHaveAttribute('data-status', 'healed');
    await expect(panel.locator('[data-testid="detail-tab"][aria-selected="true"]')).toHaveAttribute('data-tab', 'drift');
    // The verdict makes clear there is no diff.
    await expect(panel.locator('[data-testid="detail-verdict"]')).toContainText(/no diff/i);

    // The drifted service is listed with its degraded status; the healthy
    // service is not.
    const drift = panel.locator('[data-testid="heal-drift"]');
    await expect(drift.getByText('app', { exact: true })).toBeVisible();
    await expect(drift.getByText('unhealthy', { exact: true })).toBeVisible();
    await expect(drift.getByText('db', { exact: true })).toHaveCount(0);

    // Clicking the row again toggles the detail closed.
    await opener.click();
    await expect(panel).toHaveCount(0);
  });
});
