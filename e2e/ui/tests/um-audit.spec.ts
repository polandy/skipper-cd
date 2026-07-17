import { test, expect } from '../fixtures/test';

// Maske M: per-stack deploy history (audit log, ADR-0033). See dev-docs/e2e-tests.md §4.14.
//
// The newest row per stack carries a history button; clicking it opens a panel
// listing that stack's durable terminal deploy outcomes, fetched from
// /api/audit. The panel joins the one-open-panel-per-row rule (Maske L). The
// harness runs the real backend, so the records come from real deploys — the
// startup deploy plus a webhook image bump give two `success` records.
// Behaviour-only (no snapshot).

const webRows = (page: import('@playwright/test').Page) =>
  page.locator('[data-testid="deploy-row"][data-stack="web"]');

// Drive a second deploy of `web` and wait until the backend has recorded both
// outcomes, so the panel (fetched once on open) always sees the full history.
async function twoDeploys(page: import('@playwright/test').Page, skipper: import('../fixtures/harness').Skipper) {
  await page.goto(`${skipper.baseURL}/`);
  skipper.setStackImage('web', '1.26');
  expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);
  await expect
    .poll(async () => (await (await fetch(`${skipper.baseURL}/api/audit?stack=web`)).json()).length)
    .toBe(2);
  await expect(webRows(page)).toHaveCount(2);
}

// UM1 — the history panel lists the stack's terminal outcomes, newest first.
test.describe('UM1: per-stack deploy history', () => {
  test('opens from the newest row and lists past deploys', async ({ page, skipper }) => {
    await twoDeploys(page, skipper);

    const newest = webRows(page).first();
    await newest.locator('[data-testid="history-btn"]').click();

    const panel = page.locator('[data-testid="audit-panel"]');
    await expect(panel).toHaveCount(1);
    // Bound to its row (variant A: the row gains audit-open, panel is its sibling).
    await expect(newest).toHaveClass(/audit-open/);
    const sibling = await newest.evaluate((r) => r.nextElementSibling?.getAttribute('data-testid') ?? null);
    expect(sibling).toBe('audit-panel');

    // Two success records, and each carries the deployed commit's short SHA.
    const rows = panel.locator('[data-testid="audit-row"]');
    await expect(rows).toHaveCount(2);
    await expect(rows.first()).toHaveAttribute('data-status', 'success');
    await expect(rows.first().locator('.ar-sha')).not.toHaveText('—');
    await expect(rows.first().locator('.ar-status')).toContainText('success');

    // Toggling the button again closes the panel.
    await newest.locator('[data-testid="history-btn"]').click();
    await expect(panel).toHaveCount(0);
    await expect(newest).not.toHaveClass(/audit-open/);
  });
});

// UM2 — the history button lives only on the newest row of a stack.
test.describe('UM2: history button is per-stack, newest row only', () => {
  test('older rows of the same stack carry no history button', async ({ page, skipper }) => {
    await twoDeploys(page, skipper);
    await expect(webRows(page).first().locator('[data-testid="history-btn"]')).toHaveCount(1);
    await expect(webRows(page).nth(1).locator('[data-testid="history-btn"]')).toHaveCount(0);
  });
});

// UM3 — the history panel obeys one-open-panel-per-row: opening it closes an
// open diff panel on the row, and opening the diff panel closes it.
test.describe('UM3: history and diff panels are mutually exclusive', () => {
  test('opening one panel closes the other', async ({ page, skipper }) => {
    await twoDeploys(page, skipper);
    const newest = webRows(page).first();
    const auditPanel = page.locator('[data-testid="audit-panel"]');
    const diffPanel = page.locator('[data-testid="diff-panel"]');

    // History first, then diff: the diff panel replaces the history panel.
    await newest.locator('[data-testid="history-btn"]').click();
    await expect(auditPanel).toHaveCount(1);
    await newest.locator('[data-testid="files-pill"]').click();
    await expect(diffPanel).toHaveCount(1);
    await expect(auditPanel).toHaveCount(0);
    await expect(newest).toHaveClass(/diff-open/);
    await expect(newest).not.toHaveClass(/audit-open/);

    // Diff open, then history: the history panel replaces the diff panel.
    await newest.locator('[data-testid="history-btn"]').click();
    await expect(auditPanel).toHaveCount(1);
    await expect(diffPanel).toHaveCount(0);
    await expect(newest).toHaveClass(/audit-open/);
    await expect(newest).not.toHaveClass(/diff-open/);
  });
});
