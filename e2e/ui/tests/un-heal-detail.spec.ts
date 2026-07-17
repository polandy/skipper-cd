import { test, expect } from '../fixtures/test';

// Maske N: Self-heal row detail — badge + drifted-services panel (ADR-0029
// amendment). See dev-docs/e2e-tests.md §4.15.
//
// Builds on Maske K: drives the *real* self-heal loop (health poller reports the
// stack degraded → self-heal restores it → a `healed` row appears), then asserts
// the row's UI affordance. A heal is not a git deploy, so the healed row has no
// files pill; its files cell instead carries a teal self-heal badge that expands
// a detail panel explaining the corrective redeploy and listing the services
// that had drifted when it ran. Behaviour-only (no snapshot).

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

test.describe('UN1: healed row shows a self-heal badge that expands the drift detail', () => {
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

  test('the badge opens a panel explaining the heal and listing the drifted service', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);
    await expect(
      page.locator('[data-testid="deploy-row"][data-stack="web"][data-status="success"]'),
    ).toHaveCount(1);

    // Degrade the stack → self-heal runs a corrective redeploy → a healed row.
    skipper.setStackHealth('web', appDegraded);
    const row = healedRow(page);
    await expect(row).toBeVisible();
    // Recovery quiesces the loop so no further heals pile up while we interact.
    skipper.setStackHealth('web', healthyBoth);

    // A heal has no changed files: the row carries the self-heal badge, not a
    // files pill.
    const badge = row.locator('[data-testid="heal-pill"]');
    await expect(badge).toBeVisible();
    await expect(badge).toHaveText(/self-heal/);
    await expect(row.locator('[data-testid="files-pill"]')).toHaveCount(0);

    // Click the badge → the bound heal-detail panel opens below the row.
    await badge.click();
    const panel = page.locator('[data-testid="heal-panel"]');
    await expect(panel).toBeVisible();
    await expect(panel).toHaveClass(/bound/);
    await expect(panel).toHaveAttribute('data-status', 'healed');
    // The explanation makes clear there is no diff.
    await expect(panel).toContainText(/no diff/i);

    // The drifted service is listed with its degraded status; the healthy
    // service is not.
    await expect(panel.getByText('app', { exact: true })).toBeVisible();
    await expect(panel.getByText('unhealthy', { exact: true })).toBeVisible();
    await expect(panel.getByText('db', { exact: true })).toHaveCount(0);

    // Clicking the badge again toggles the panel closed (one panel per row).
    await badge.click();
    await expect(page.locator('[data-testid="heal-panel"]')).toHaveCount(0);
  });
});

test.describe('UN2: the heal panel obeys the one-open-panel-per-row rule', () => {
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

  test('opening the heal panel closes an open health panel on the same row', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);

    skipper.setStackHealth('web', appDegraded);
    const row = healedRow(page);
    await expect(row).toBeVisible();
    skipper.setStackHealth('web', healthyBoth);

    // Open the stack-health panel via the health pill on the (newest) healed row.
    await row.locator('[data-testid="health-pill"]').click();
    await expect(page.locator('[data-testid="health-panel"]')).toBeVisible();

    // Opening the heal panel replaces it — never two panels under one row.
    await row.locator('[data-testid="heal-pill"]').click();
    await expect(page.locator('[data-testid="heal-panel"]')).toBeVisible();
    await expect(page.locator('[data-testid="health-panel"]')).toHaveCount(0);
  });
});
