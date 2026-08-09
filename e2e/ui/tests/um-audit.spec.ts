import { test, expect } from '../fixtures/test';
import { openRowMenu } from '../fixtures/menu';

// Maske M: per-stack deploy history (audit log, ADR-0033). See dev-docs/e2e-tests.md §4.14.
//
// The history button lives inside the newest row's ⋯ overflow menu (T3.13), so
// each history-btn interaction opens that menu first (openRowMenu).
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
    await openRowMenu(newest);
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

    // Toggling the button again closes the panel (reopen the menu to reach it).
    await openRowMenu(newest);
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

    // History first, then diff: the diff panel replaces the history panel. The
    // files pill stays on the row itself, so only history needs the menu opened.
    await openRowMenu(newest);
    await newest.locator('[data-testid="history-btn"]').click();
    await expect(auditPanel).toHaveCount(1);
    await newest.locator('[data-testid="files-pill"]').click();
    await expect(diffPanel).toHaveCount(1);
    await expect(auditPanel).toHaveCount(0);
    await expect(newest).toHaveClass(/diff-open/);
    await expect(newest).not.toHaveClass(/audit-open/);

    // Diff open, then history: the history panel replaces the diff panel.
    await openRowMenu(newest);
    await newest.locator('[data-testid="history-btn"]').click();
    await expect(auditPanel).toHaveCount(1);
    await expect(diffPanel).toHaveCount(0);
    await expect(newest).toHaveClass(/audit-open/);
    await expect(newest).not.toHaveClass(/diff-open/);
  });
});

// UM4 — a queued row is replaced when its stack deploys; an open history panel
// must go with it instead of being left orphaned in the table (a queued row is
// the newest row of its stack, so it carries the history button).
test.describe('UM4: history panel does not outlive a queued row', () => {
  test('draining a queued row removes its open history panel', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);

    // Pause autosync, push a change → a queued row for web, newest of its stack.
    expect(await skipper.postAutosync('', false)).toBe(200);
    skipper.setStackImage('web', '1.26');
    expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);
    const queuedRow = page.locator('[data-testid="deploy-row"][data-stack="web"][data-status="queued"]');
    await expect(queuedRow).toHaveCount(1);

    await openRowMenu(queuedRow);
    await queuedRow.locator('[data-testid="history-btn"]').click();
    await expect(page.locator('[data-testid="audit-panel"]')).toHaveCount(1);

    // Resume: the queued row is superseded by the real deploy — the panel it
    // anchored must be removed with it, not survive as a sibling of the new row.
    expect(await skipper.postAutosync('', true)).toBe(200);
    await expect(queuedRow).toHaveCount(0);
    await expect(
      page.locator('[data-testid="deploy-row"][data-stack="web"][data-status="success"]').first(),
    ).toBeVisible();
    await expect(page.locator('[data-testid="audit-panel"]')).toHaveCount(0);
  });
});

// UM5 — the history folds runs of routine outcomes (UI_SPEC "Deploy history"):
// a long-lived stack converges the same way for weeks, so rendered verbatim its
// history is one line repeated. Nothing is lost — the toggle swaps in the
// verbatim list, and the deploys inside a run stay reachable as commit chips.
test.describe('UM5: routine outcomes fold, verbatim list behind the toggle', () => {
  test('a run of successes becomes one summary line the toggle unfolds', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);

    // Five deploys of one stack: the startup deploy plus four image bumps. The
    // audit log is polled to five, so the panel opens on a settled history.
    for (const tag of ['1.26', '1.27', '1.28', '1.29']) {
      skipper.setStackImage('web', tag);
      expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);
    }
    await expect
      .poll(async () => (await (await fetch(`${skipper.baseURL}/api/audit?stack=web`)).json()).length)
      .toBe(5);

    const newest = webRows(page).first();
    await openRowMenu(newest);
    await newest.locator('[data-testid="history-btn"]').click();
    const panel = page.locator('[data-testid="audit-panel"]');

    // The newest record keeps its own line; the four older ones fold into one.
    await expect(panel.locator('[data-testid="audit-row"]')).toHaveCount(1);
    const fold = panel.locator('[data-testid="audit-fold"]');
    await expect(fold).toHaveAttribute('data-status', 'success');
    await expect(fold).toContainText('4 more successful deploys since');
    await expect(fold.locator('[data-testid="audit-fold-commit"]')).toHaveCount(3);

    // The toggle swaps in the verbatim list: every record, no fold line.
    const toggle = panel.locator('[data-testid="audit-fold-toggle"]');
    await expect(toggle).toHaveText('all 5 deploys');
    await expect(toggle).toHaveAttribute('aria-expanded', 'false');
    await toggle.click();
    await expect(toggle).toHaveAttribute('aria-expanded', 'true');
    await expect(toggle).toHaveText('fold routine outcomes');
    await expect(panel.locator('.ap-raw .audit-row')).toHaveCount(5);
    await expect(fold).toBeHidden();
    // aria-controls names the list the toggle revealed.
    const controls = await toggle.getAttribute('aria-controls');
    await expect(page.locator(`#${controls}`)).toBeVisible();

    // …and back.
    await toggle.click();
    await expect(fold).toBeVisible();
    await expect(toggle).toHaveText('all 5 deploys');
  });
});

// UM6 — a history with nothing routine to fold renders verbatim: no fold line
// and no toggle. The fold must not add a control to every stack regardless of
// whether it collapsed anything.
test.describe('UM6: a short history keeps every row and offers no toggle', () => {
  test('two deploys render as two rows', async ({ page, skipper }) => {
    await twoDeploys(page, skipper);
    const newest = webRows(page).first();
    await openRowMenu(newest);
    await newest.locator('[data-testid="history-btn"]').click();
    const panel = page.locator('[data-testid="audit-panel"]');
    await expect(panel.locator('[data-testid="audit-row"]')).toHaveCount(2);
    await expect(panel.locator('[data-testid="audit-fold"]')).toHaveCount(0);
    await expect(panel.locator('[data-testid="audit-fold-toggle"]')).toHaveCount(0);
  });
});
