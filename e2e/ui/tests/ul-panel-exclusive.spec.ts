import { test, expect } from '../fixtures/test';
import { detailHome } from '../fixtures/stack-detail';

// Maske L: one open detail per deploy table. See docs/dev/e2e-tests.md §4.13.
//
// A deploy row opens exactly one panel, its deploy detail (the health panel
// moved to the Stacks detail), and the table keeps at most one detail open.
// Behaviour-only (no snapshot).

// UL2 — the rule is one open detail per table, not per row: opening a panel on
// another row closes the first row's panel and drops its binding.
test.describe('UL2: a panel on another row closes the open one', () => {
  test.use({ startOptions: { stacks: ['web', 'api'] } });

  test('opening the second row panel closes the first', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);

    // One bump run puts a diff on the newest row of each stack.
    skipper.setStackImage('web', '1.26');
    skipper.setStackImage('api', '1.26');
    expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);
    const changed = (stack: string) =>
      page.locator(`[data-testid="deploy-row"][data-stack="${stack}"][data-has-diffs="1"]`);
    await expect(changed('web')).toHaveCount(1);
    await expect(changed('api')).toHaveCount(1);

    const diffPanel = page.locator('[data-testid="detail-panel"][data-kind="deploy"]');
    await changed('web').locator('.stack-name').click();
    await expect(diffPanel).toHaveCount(1);
    await expect(changed('web')).toHaveClass(/audit-open/);

    await changed('api').locator('.stack-name').click();
    await expect(changed('api')).toHaveClass(/audit-open/);
    await expect(changed('web')).not.toHaveClass(/audit-open/);
    await expect(diffPanel).toHaveCount(1);
    expect(await detailHome(changed('api'))).toBe('side');
  });
});
