import { test, expect } from '../fixtures/test';

// Maske I: Diff-panel commit metadata + variant-A row binding. See
// dev-docs/e2e-tests.md §4.10.
//
// A webhook that bumps a stack's image commits against the startup commit, so
// the deploy's diff panel carries both a real diff and commit metadata. The
// harness commits as author `e2e` with the message `bump <stack> to <tag>`, so
// the rendered header is deterministic — no real docker, no real containers.
// Behaviour-only (no snapshot; UA8 already snapshots the diff colouring).

const diffRow = (page: import('@playwright/test').Page) =>
  page.locator('[data-testid="deploy-row"][data-stack="web"][data-status="success"][data-has-diffs="1"]');
const filesPill = (page: import('@playwright/test').Page) => diffRow(page).locator('[data-testid="files-pill"]');
const diffPanel = (page: import('@playwright/test').Page) => page.locator('[data-testid="diff-panel"]');
const diffHead = (page: import('@playwright/test').Page) => page.locator('[data-testid="diff-head"]');

// UI1 — the diff panel's commit header echoes the row (stack + status) and shows
// the deployed commit's message, author and short SHA.
test.describe('UI1: commit metadata header', () => {
  test('the header echoes the row and shows the commit message + author', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);

    skipper.setStackImage('web', '1.26');
    expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);
    await expect(diffRow(page)).toHaveCount(1);

    await filesPill(page).click();
    await expect(diffPanel(page)).toHaveCount(1);

    const head = diffHead(page);
    await expect(head).toBeVisible();
    // Row echo: the panel names its own row (stack + "Deploy diff" + status pill).
    await expect(head.locator('.dh-who')).toHaveText('web');
    await expect(head.locator('.dh-label')).toHaveText('Deploy diff');
    await expect(head.locator('.dh-pill')).toContainText('success');
    // Commit metadata: subject, author, and a 7-char short SHA.
    await expect(head.locator('.dh-subject')).toHaveText('bump web to 1.26');
    await expect(head.locator('.diff-meta-line')).toContainText('e2e');
    const sha = await head.locator('.m-sha').first().innerText();
    expect(sha).toMatch(/^[0-9a-f]{7}$/);
  });
});

// UI2 — opening the diff panel binds it to its row (variant A): the row is marked
// open and the panel is bound + status-tagged, both cleared when it closes.
test.describe('UI2: variant-A row binding', () => {
  test('the row and panel share the open/bound state and it clears on close', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);

    skipper.setStackImage('web', '1.26');
    expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);
    await expect(diffRow(page)).toHaveCount(1);

    await filesPill(page).click();
    const panel = diffPanel(page);
    await expect(panel).toHaveCount(1);
    await expect(panel).toHaveClass(/bound/);
    await expect(panel).toHaveAttribute('data-status', 'success');
    await expect(diffRow(page)).toHaveClass(/diff-open/);
    // The panel is the row's direct sibling, so the shared left bar is unbroken.
    const siblingTestid = await diffRow(page).evaluate(
      (row) => row.nextElementSibling?.getAttribute('data-testid') ?? null,
    );
    expect(siblingTestid).toBe('diff-panel');

    await filesPill(page).click();
    await expect(panel).toHaveCount(0);
    await expect(diffRow(page)).not.toHaveClass(/diff-open/);
  });
});

// UI3 — a deploy spanning several commits shows the newest as the headline and an
// `N commits` pill that toggles the full per-commit list.
test.describe('UI3: multi-commit range', () => {
  test('the pill toggles the commit list; the newest commit heads the panel', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);

    // Two commits land before a single deploy: the range carries both.
    skipper.setStackImage('web', '1.26');
    skipper.setStackImage('web', '1.27');
    expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);
    await expect(diffRow(page)).toHaveCount(1);

    await filesPill(page).click();
    const head = diffHead(page);
    await expect(head).toBeVisible();
    // Newest commit heads the panel.
    await expect(head.locator('.dh-subject')).toHaveText('bump web to 1.27');

    const pill = head.locator('[data-testid="commits-pill"]');
    await expect(pill).toContainText('2 commits');

    // The list is collapsed until the pill is clicked, then lists both commits.
    const list = head.locator('[data-testid="diff-commit-list"]');
    await expect(list).toBeHidden();
    await pill.click();
    await expect(list).toBeVisible();
    await expect(list.locator('li')).toHaveCount(2);
    await expect(list).toContainText('bump web to 1.27');
    await expect(list).toContainText('bump web to 1.26');
  });
});
