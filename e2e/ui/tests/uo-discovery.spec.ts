import { test, expect } from '../fixtures/test';

// Maske O: stack discovery surface (ADR-0034). See dev-docs/e2e-tests.md §4.16.
//
// The harness boots in discovery mode: the origin's stack dirs are the stack
// set and the repo-root skipper.yaml carries the per-stack overrides. Covers
// the disabled-stacks line and the failed _config row with its marked
// skipper.yaml excerpt. Behaviour-only (no snapshot).

// UO1 — parked stacks render on the disabled line, not in the table.
test.describe('UO1: disabled-stacks line', () => {
  test.use({
    startOptions: {
      stacks: ['web', 'wip'],
      discovery: { repoConfig: 'stacks:\n  wip:\n    disabled: true\n', disabled: ['wip'] },
    },
  });

  test('lists parked stacks below the table, in the deploys view only', async ({
    page,
    skipper,
  }) => {
    await page.goto(`${skipper.baseURL}/`);

    // web deployed normally; wip is parked — no row, one chip.
    await expect(page.locator('[data-testid="deploy-row"][data-stack="web"]')).toHaveCount(1);
    await expect(page.locator('[data-testid="deploy-row"][data-stack="wip"]')).toHaveCount(0);
    const line = page.locator('[data-testid="disabled-stacks"]');
    await expect(line).toBeVisible();
    await expect(line.locator('.dis-chip')).toHaveText(['wip']);

    // The line belongs to the deploys view: gone in logs, back with deploys.
    await page.locator('[data-testid="view-toggle"] button[data-view="logs"]').click();
    await expect(line).toBeHidden();
    await page.locator('[data-testid="view-toggle"] button[data-view="deploys"]').click();
    await expect(line).toBeVisible();
  });
});

// UO2 — a broken repo skipper.yaml fails as a _config row whose error panel
// shows the marked excerpt of the offending line.
test.describe('UO2: broken repo config', () => {
  test.use({
    startOptions: { stacks: ['web'], discovery: {} },
  });

  test('failed _config row carries the marked skipper.yaml excerpt', async ({
    page,
    skipper,
  }) => {
    await page.goto(`${skipper.baseURL}/`);
    await expect(page.locator('[data-testid="deploy-row"][data-stack="web"]')).toHaveCount(1);
    // Healthy discovery run: no disabled stacks, so the line stays hidden.
    await expect(page.locator('[data-testid="disabled-stacks"]')).toBeHidden();

    // Push a skipper.yaml whose line 3 is broken (stray tab).
    skipper.setRepoConfig('stacks:\n  web:\n\ticon: broken\n');
    expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);

    const row = page.locator('[data-testid="deploy-row"][data-stack="_config"]');
    await expect(row).toHaveCount(1);
    await expect(row).toHaveAttribute('data-status', 'failed');
    const panel = page.locator('[data-testid="error-panel"]');
    await expect(panel).toContainText('parse skipper.yaml');
    await expect(panel).toContainText('> 3 |');
  });
});
