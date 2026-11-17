import { test, expect } from '../fixtures/test';

// Maske I: deploy detail commit facts + row binding. See
// docs/dev/e2e-tests.md §4.10.
//
// A webhook that bumps a stack's image commits against the startup commit, so
// the deploy's detail carries both a real diff and commit metadata. The harness
// commits as author `e2e` with the message `bump <stack> to <tag>`, so the
// rendered facts are deterministic — no real docker, no real containers.
// Behaviour-only (no snapshot).

const diffRow = (page: import('@playwright/test').Page) =>
  page.locator('[data-testid="deploy-row"][data-stack="web"][data-status="success"][data-has-diffs="1"]');
const filesPill = (page: import('@playwright/test').Page) => diffRow(page).locator('[data-testid="files-pill"]');
const panel = (page: import('@playwright/test').Page) =>
  page.locator('[data-testid="detail-panel"][data-kind="deploy"]');
const facts = (page: import('@playwright/test').Page) => panel(page).locator('[data-testid="detail-facts"]');

// UI1 — the Commit fact fills in from the lazy diff fetch: the deployed
// commit's short SHA and subject. The head names the row's stack and status.
test.describe('UI1: commit facts', () => {
  test('the Commit fact shows the short SHA and subject', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);

    skipper.setStackImage('web', '1.26');
    expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);
    await expect(diffRow(page)).toHaveCount(1);

    await filesPill(page).click();
    await expect(panel(page).locator('[data-testid="diff-panel"]')).toHaveCount(1);

    await expect(panel(page).locator('.dp-name')).toHaveText('web');
    await expect(panel(page).locator('.dp-head [data-testid="status-badge"]')).toContainText('success');
    await expect(facts(page).locator('.dp-subject')).toHaveText('bump web to 1.26');
    const sha = await facts(page).locator('.dp-sha').first().innerText();
    expect(sha).toMatch(/^[0-9a-f]{7}$/);
  });
});

// UI2 — opening the detail marks its row open; both clear when it closes.
test.describe('UI2: row binding', () => {
  test('the row is marked open while its detail shows and it clears on close', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);

    skipper.setStackImage('web', '1.26');
    expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);
    await expect(diffRow(page)).toHaveCount(1);

    await filesPill(page).click();
    await expect(panel(page)).toHaveCount(1);
    await expect(panel(page)).toHaveAttribute('data-status', 'success');
    await expect(diffRow(page)).toHaveClass(/audit-open/);
    // The panel is the row's direct sibling, so the left bar is unbroken.
    const siblingTestid = await diffRow(page).evaluate(
      (row) => row.nextElementSibling?.getAttribute('data-testid') ?? null,
    );
    expect(siblingTestid).toBe('detail-panel');

    // The head's close button closes it like a second click would.
    await panel(page).locator('[data-testid="detail-close"]').click();
    await expect(panel(page)).toHaveCount(0);
    await expect(diffRow(page)).not.toHaveClass(/audit-open/);
  });
});

// UI3 — a deploy spanning several commits shows the newest in its Commit fact
// and a Commits tab listing every commit.
test.describe('UI3: multi-commit range', () => {
  test('a Commits tab lists the range; the newest commit heads the facts', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);

    // Two commits land before a single deploy: the range carries both.
    skipper.setStackImage('web', '1.26');
    skipper.setStackImage('web', '1.27');
    expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);
    await expect(diffRow(page)).toHaveCount(1);

    await filesPill(page).click();
    // Newest commit heads the Commit fact.
    await expect(facts(page).locator('.dp-subject')).toHaveText('bump web to 1.27');

    // A Commits tab appears only for a multi-commit range, and lists both.
    const tab = panel(page).locator('[data-testid="detail-tab"][data-tab="commits"]');
    await expect(tab).toContainText('2');
    await tab.click();
    const list = panel(page).locator('[data-testid="diff-commit-list"]');
    await expect(list).toBeVisible();
    await expect(list.locator('li')).toHaveCount(2);
    await expect(list).toContainText('bump web to 1.27');
    await expect(list).toContainText('bump web to 1.26');
  });
});
