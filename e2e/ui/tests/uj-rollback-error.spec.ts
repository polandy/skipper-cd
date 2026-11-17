import { test, expect } from '../fixtures/test';

// Maske J: rolled-back deploy detail — verdict before diff. See
// docs/dev/e2e-tests.md §4.11.
//
// A rolled-back deploy (startup up#1 succeeds and sets LastDeployedCommit; the
// deploy's up#2 fails and the rollback up#3 succeeds → rolled_back). The
// backend carries the deploy's changed files + diffs on the terminal
// rolled_back event, so its row is diffable; the one deploy detail shows the
// rollback verdict above the diff. Behaviour-only (no snapshot).
test.describe('Maske J: rolled-back deploy detail', () => {
  test.use({ startOptions: { stacks: ['web'], stubEnv: { STUB_DOCKER_FAIL_NTH_UP: '2' } } });

  const webRow = (page: import('@playwright/test').Page, status: string) =>
    page.locator(`[data-testid="deploy-row"][data-stack="web"][data-status="${status}"]`);
  const panel = (page: import('@playwright/test').Page) =>
    page.locator('[data-testid="detail-panel"][data-kind="deploy"]');

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

  // UJ1 — no error box rides the list: the row stays closed until clicked, then
  // its detail is the row's direct sibling and carries the row's status.
  test('UJ1: the detail opens on click as the row’s direct sibling', async ({ page, skipper }) => {
    const row = await rollBack(page, skipper);

    await expect(panel(page)).toHaveCount(0);
    await row.click();
    await expect(panel(page)).toBeVisible();
    await expect(panel(page)).toHaveAttribute('data-status', 'rolled_back');
    await expect(row).toHaveClass(/audit-open/);
    const sibling = await row.evaluate(
      (r) => r.nextElementSibling?.getAttribute('data-testid') ?? null,
    );
    expect(sibling).toBe('detail-panel');
  });

  // UJ2 — the rolled-back event carries a diff; the detail shows the rollback
  // verdict first and the diff in its Changes tab, in one panel.
  test('UJ2: the rollback verdict comes before the diff, in one panel', async ({
    page,
    skipper,
  }) => {
    const row = await rollBack(page, skipper);

    // The deploy fix: a rolled-back deploy reports its diff, not just the path.
    await expect(row).toHaveAttribute('data-has-diffs', '1');

    await row.locator('.stack-name').click();
    await expect(panel(page)).toHaveCount(1);
    const verdict = panel(page).locator('[data-testid="detail-verdict"]');
    await expect(verdict).toContainText('Rolled back to app 1.25');
    await expect(verdict).toContainText('docker compose up');
    const diffPanel = panel(page).locator('[data-testid="diff-panel"]');
    await expect(diffPanel).toBeVisible();

    // The cause precedes the diff in document order.
    const verdictFirst = await verdict.evaluate((v) => {
      const diff = document.querySelector('[data-testid="detail-panel"] [data-testid="diff-panel"]');
      return !!diff && !!(v.compareDocumentPosition(diff) & Node.DOCUMENT_POSITION_FOLLOWING);
    });
    expect(verdictFirst).toBe(true);
  });
});
