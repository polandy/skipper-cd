import { test, expect } from '../fixtures/test';

// Maske J: rolled-back error panel — variant-A binding + diff. See
// dev-docs/e2e-tests.md §4.11.
//
// A rolled-back deploy (startup up#1 succeeds and sets LastDeployedCommit; the
// deploy's up#2 fails and the rollback up#3 succeeds → rolled_back). The
// backend now carries the deploy's changed files + diffs on the terminal
// rolled_back event, so its row is diffable and its error box binds to the row
// like the diff/health panels. Behaviour-only (no snapshot).
test.describe('Maske J: rolled-back error panel', () => {
  test.use({ startOptions: { stacks: ['web'], stubEnv: { STUB_DOCKER_FAIL_NTH_UP: '2' } } });

  const webRow = (page: import('@playwright/test').Page, status: string) =>
    page.locator(`[data-testid="deploy-row"][data-stack="web"][data-status="${status}"]`);
  const errorPanel = (page: import('@playwright/test').Page) =>
    page.locator('[data-testid="error-panel"]');

  const rollBack = async (
    page: import('@playwright/test').Page,
    skipper: import('../fixtures/harness').Skipper,
  ) => {
    await page.goto(`${skipper.baseURL}/`);
    await expect(webRow(page, 'success')).toHaveCount(1); // startup settled
    skipper.setStackImage('web', '1.26');
    expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);
    const row = webRow(page, 'rolled_back');
    await expect(row).toHaveCount(1);
    return row;
  };

  // UJ1 — the error box binds to its rolled-back row (variant A): it carries the
  // row's status and is the row's direct sibling, so the shared left bar is
  // unbroken (no card floating between rows).
  test('UJ1: the error panel is bound to its row and is its direct sibling', async ({
    page,
    skipper,
  }) => {
    const row = await rollBack(page, skipper);

    await expect(errorPanel(page)).toBeVisible();
    await expect(errorPanel(page)).toHaveAttribute('data-status', 'rolled_back');
    const sibling = await row.evaluate(
      (r) => r.nextElementSibling?.getAttribute('data-testid') ?? null,
    );
    expect(sibling).toBe('error-panel');
  });

  // UJ2 — the rolled-back event now carries a diff, so the row is diffable; opening
  // it inserts the diff panel between the row and its error box, keeping the bar
  // continuous across row → panel → error.
  test('UJ2: opening the diff keeps the bar continuous through row → panel → error', async ({
    page,
    skipper,
  }) => {
    const row = await rollBack(page, skipper);

    // The deploy fix: a rolled-back deploy reports its diff, not just the path.
    await expect(row).toHaveAttribute('data-has-diffs', '1');

    await row.locator('[data-testid="files-pill"]').click();
    const diffPanel = page.locator('[data-testid="diff-panel"]');
    await expect(diffPanel).toHaveCount(1);
    await expect(diffPanel).toHaveClass(/bound/);
    await expect(diffPanel).toHaveAttribute('data-status', 'rolled_back');
    await expect(row).toHaveClass(/diff-open/);

    // DOM order row → diff-panel → error-panel: all three share one unbroken bar.
    const order = await row.evaluate((r) => {
      const a = r.nextElementSibling;
      const b = a?.nextElementSibling;
      return [a?.getAttribute('data-testid') ?? null, b?.getAttribute('data-testid') ?? null];
    });
    expect(order).toEqual(['diff-panel', 'error-panel']);
  });
});
