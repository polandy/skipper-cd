import { test, expect } from '../fixtures/test';

// Maske L: one open detail per deploy table. See docs/dev/e2e-tests.md §4.13.
//
// The health panel and the deploy detail are mutually exclusive on a row:
// opening one closes the other, so the resulting layout never depends on the
// click order and the variant-A row binding is never split across two panels.
// A webhook image bump puts a diff on the newest row while the health poller
// (scripted via skipper.setStackHealth) puts the health pill on the same row.
// Behaviour-only (no snapshot).

const row = (page: import('@playwright/test').Page) =>
  page.locator('[data-testid="deploy-row"][data-stack="web"][data-has-diffs="1"]');

// UL1 — whichever panel opens second replaces the first, in both orders, and the
// surviving panel is always the row's direct sibling.
test.describe('UL1: health panel and deploy detail are mutually exclusive', () => {
  test.use({ startOptions: { stacks: ['web'], healthPoll: 1 } });

  test('opening one panel closes the other, regardless of click order', async ({ page, skipper }) => {
    skipper.setStackHealth('web', [{ Service: 'app', State: 'running', Health: 'healthy' }]);
    await page.goto(`${skipper.baseURL}/`);

    // A bump deploy puts the files pill and the health pill on one row.
    skipper.setStackImage('web', '1.26');
    expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);
    await expect(row(page)).toHaveCount(1);
    const healthPill = row(page).locator('[data-testid="health-pill"]');
    await expect(healthPill).toHaveAttribute('data-health', 'healthy');

    const healthPanel = page.locator('[data-testid="health-panel"]');
    const diffPanel = page.locator('[data-testid="detail-panel"][data-kind="deploy"]');

    // Health first, then diff: the deploy detail replaces the health panel.
    await healthPill.click();
    await expect(healthPanel).toHaveCount(1);
    await row(page).locator('[data-testid="files-pill"]').click();
    await expect(diffPanel).toHaveCount(1);
    await expect(healthPanel).toHaveCount(0);
    await expect(row(page)).toHaveClass(/audit-open/);
    await expect(row(page)).not.toHaveClass(/health-open/);

    // Detail open, then health: the health panel replaces the deploy detail.
    await healthPill.click();
    await expect(healthPanel).toHaveCount(1);
    await expect(diffPanel).toHaveCount(0);
    await expect(row(page)).toHaveClass(/health-open/);
    await expect(row(page)).not.toHaveClass(/audit-open/);

    // The surviving panel is the row's direct sibling (unbroken left bar).
    const sibling = await row(page).evaluate(
      (r) => r.nextElementSibling?.getAttribute('data-testid') ?? null,
    );
    expect(sibling).toBe('health-panel');
  });
});

// UL2 — the rule is one open detail per table, not per row: opening a panel on
// another row closes the first row's panel and drops its binding.
test.describe('UL2: a panel on another row closes the open one', () => {
  test.use({ startOptions: { stacks: ['web', 'api'] } });

  test('opening the second row panel closes the first', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);

    // One bump run puts a files pill on the newest row of each stack.
    skipper.setStackImage('web', '1.26');
    skipper.setStackImage('api', '1.26');
    expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);
    const changed = (stack: string) =>
      page.locator(`[data-testid="deploy-row"][data-stack="${stack}"][data-has-diffs="1"]`);
    await expect(changed('web')).toHaveCount(1);
    await expect(changed('api')).toHaveCount(1);

    const diffPanel = page.locator('[data-testid="detail-panel"][data-kind="deploy"]');
    await changed('web').locator('[data-testid="files-pill"]').click();
    await expect(diffPanel).toHaveCount(1);
    await expect(changed('web')).toHaveClass(/audit-open/);

    await changed('api').locator('[data-testid="files-pill"]').click();
    await expect(changed('api')).toHaveClass(/audit-open/);
    await expect(changed('web')).not.toHaveClass(/audit-open/);
    await expect(diffPanel).toHaveCount(1);
    const sibling = await changed('api').evaluate(
      (r) => r.nextElementSibling?.getAttribute('data-testid') ?? null,
    );
    expect(sibling).toBe('detail-panel');
  });
});
