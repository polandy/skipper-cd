import { test, expect } from '../fixtures/test';

// Maske A: Deploys-View. See docs/e2e-tests.md §4.2.

// UA1 — Row lifecycle. A stack that deploys (held in `deploying`, then released
// to `success`) appears as one newest-first row that mutates in place — not a
// duplicate. This is also the UI-harness smoke test: it proves the browser
// renders live SSE deploy state driven by the real backend.
test('UA1: deploy row transitions deploying → success in place', async ({ page, skipper }) => {
  await page.goto(`${skipper.baseURL}/`);

  const rows = page.locator('[data-testid="deploy-row"][data-stack="web"]');

  // The startup deploy already produced one success row for `web`.
  await expect(rows).toHaveCount(1);
  await expect(rows.first()).toHaveAttribute('data-status', 'success');

  // Hold the next `up` so the `deploying` state is observable, then push a change.
  skipper.hold();
  skipper.setStackImage('web', '1.26');
  expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);

  // A new row is prepended (newest first) and shows `deploying`.
  await expect(rows.first()).toHaveAttribute('data-status', 'deploying');
  await expect(rows.first().locator('[data-testid="status-badge"]')).toContainText('deploying');
  await expect(rows).toHaveCount(2);

  // Release the `up`: the same row mutates in place to `success` — still 2 rows,
  // no third row appended.
  skipper.release();
  await expect(rows.first()).toHaveAttribute('data-status', 'success');
  await expect(rows).toHaveCount(2);
});

// UA2 — Status badges. Each deploy status the UI renders is driven through the
// real backend (recipes in docs/e2e-tests.md §3) and its row asserted to carry
// the right data-status and status-badge. One focused test per status keeps the
// backend setup for each isolated in its own instance.
test.describe('UA2: status badges', () => {
  const webRow = (page: import('@playwright/test').Page, status: string) =>
    page.locator(`[data-testid="deploy-row"][data-stack="web"][data-status="${status}"]`);

  test('success', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);
    const row = webRow(page, 'success');
    await expect(row).toHaveCount(1);
    await expect(row.locator('[data-testid="status-badge"]')).toHaveText('success');
  });

  test('skipped', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);
    await expect(webRow(page, 'success')).toHaveCount(1); // startup settled
    // Skipped rows are hidden by default; reveal them, then webhook with no new
    // commit so the unchanged stack is skipped.
    await page.locator('[data-testid="skip-filter"]').click();
    expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);

    const row = webRow(page, 'skipped');
    await expect(row).toHaveCount(1);
    await expect(row.locator('[data-testid="status-badge"]')).toHaveText('skipped');
  });

  test('deploying', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);
    await expect(webRow(page, 'success')).toHaveCount(1);

    skipper.hold();
    skipper.setStackImage('web', '1.26');
    expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);

    const badge = webRow(page, 'deploying').locator('[data-testid="status-badge"]');
    await expect(badge).toContainText('deploying');
    await expect(badge.locator('.spinner')).toBeVisible();

    skipper.release(); // let the deploy finish so teardown is clean
  });

  test('queued', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);
    await expect(webRow(page, 'success')).toHaveCount(1);

    expect(await skipper.postAutosync('', false)).toBe(200); // pause globally
    skipper.setStackImage('web', '1.26');
    expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);

    const row = webRow(page, 'queued');
    await expect(row).toHaveCount(1);
    await expect(row.locator('[data-testid="status-badge"]')).toHaveText('queued');
  });

  test.describe('rolled_back', () => {
    // Startup up#1 succeeds (sets LastDeployedCommit); the deploy's up#2 fails and
    // the rollback up#3 succeeds → rolled_back.
    test.use({ startOptions: { stacks: ['web'], stubEnv: { STUB_DOCKER_FAIL_NTH_UP: '2' } } });

    test('rolled_back', async ({ page, skipper }) => {
      await page.goto(`${skipper.baseURL}/`);
      await expect(webRow(page, 'success')).toHaveCount(1);

      skipper.setStackImage('web', '1.26');
      expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);

      const row = webRow(page, 'rolled_back');
      await expect(row).toHaveCount(1);
      await expect(row.locator('[data-testid="status-badge"]')).toHaveText('rolled back');
    });
  });

  test.describe('failed', () => {
    // The stack's very first (startup) deploy fails on `up` with no prior commit
    // to roll back to → failed. /healthz stays 503, so only wait for listening.
    test.use({
      startOptions: { stacks: ['web'], stubEnv: { STUB_DOCKER_FAIL_ON: 'up' }, readiness: 'listening' },
    });

    test('failed', async ({ page, skipper }) => {
      await page.goto(`${skipper.baseURL}/`);

      const row = webRow(page, 'failed');
      await expect(row).toHaveCount(1);
      await expect(row.locator('[data-testid="status-badge"]')).toHaveText('failed');
      // A failed deploy expands an error panel below the row.
      await expect(page.locator('[data-testid="error-panel"]')).toBeVisible();
    });
  });
});

// UA3 — Skip filter. The filter hides `skipped` deploy rows and defaults to on.
// The choice persists in localStorage('hideSkipped') so a reload restores it, and
// the button reflects the state via its `active` class. Skipped events are
// live-only (never replayed from history), so each phase produces a fresh skipped
// row with a no-change webhook rather than expecting one to survive a reload.
test.describe('UA3: skip filter', () => {
  const skippedRow = (page: import('@playwright/test').Page) =>
    page.locator('[data-testid="deploy-row"][data-stack="web"][data-status="skipped"]');
  const successRow = (page: import('@playwright/test').Page) =>
    page.locator('[data-testid="deploy-row"][data-stack="web"][data-status="success"]');
  const skipFilter = (page: import('@playwright/test').Page) =>
    page.locator('[data-testid="skip-filter"]');

  // A webhook with no new commit leaves the stack unchanged → it is skipped.
  const produceSkip = async (skipper: { sendWebhook(ref: string): Promise<number> }) =>
    expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);

  test('hidden by default; toggling reveals then hides again', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);
    await expect(successRow(page)).toHaveCount(1); // startup settled

    await produceSkip(skipper);

    // The skipped row is in the DOM but hidden by the default-on filter.
    const row = skippedRow(page);
    await expect(row).toHaveCount(1);
    await expect(row).toBeHidden();
    await expect(skipFilter(page)).toHaveClass(/\bactive\b/);

    // Toggle off → skipped rows revealed.
    await skipFilter(page).click();
    await expect(row).toBeVisible();
    await expect(skipFilter(page)).not.toHaveClass(/\bactive\b/);

    // Toggle back on → hidden again.
    await skipFilter(page).click();
    await expect(row).toBeHidden();
    await expect(skipFilter(page)).toHaveClass(/\bactive\b/);
  });

  test('the toggle choice persists across reload, both directions', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);
    await expect(successRow(page)).toHaveCount(1);

    // Reveal skipped rows and persist the choice (hideSkipped=false).
    await skipFilter(page).click();
    await expect(skipFilter(page)).not.toHaveClass(/\bactive\b/);

    await page.reload();
    // The persisted choice is applied on load, before any rows render.
    await expect(skipFilter(page)).not.toHaveClass(/\bactive\b/);
    // A fresh skip is visible without re-toggling.
    await produceSkip(skipper);
    await expect(skippedRow(page)).toHaveCount(1);
    await expect(skippedRow(page)).toBeVisible();

    // Hide again and persist (hideSkipped=true).
    await skipFilter(page).click();
    await expect(skipFilter(page)).toHaveClass(/\bactive\b/);

    await page.reload();
    await expect(skipFilter(page)).toHaveClass(/\bactive\b/);
    await produceSkip(skipper);
    await expect(skippedRow(page)).toHaveCount(1);
    await expect(skippedRow(page)).toBeHidden();
  });
});

// UA4 — Time mode. The toggle switches Time cells between relative ("just now" /
// "5s ago", the app's own format) and absolute (`toLocaleString`), defaulting to
// relative. The choice persists in localStorage('timeMode') so a reload restores
// it, and the button reflects the state via its `active` class. The startup
// success row already carries a time-cell, so no webhook is needed. Assertions on
// the cell text use the locale-independent relative pattern (/ago|just now/): the
// absolute form never matches it, whatever the browser locale.
test.describe('UA4: time mode', () => {
  const successRow = (page: import('@playwright/test').Page) =>
    page.locator('[data-testid="deploy-row"][data-stack="web"][data-status="success"]');
  const timeCell = (page: import('@playwright/test').Page) =>
    page.locator('[data-testid="time-cell"]').first();
  const timeMode = (page: import('@playwright/test').Page) =>
    page.locator('[data-testid="time-mode"]');

  test('toggling switches Time cells relative ↔ absolute', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);
    await expect(successRow(page)).toHaveCount(1); // startup settled

    // Default is relative.
    await expect(timeMode(page)).not.toHaveClass(/\bactive\b/);
    await expect(timeCell(page)).toHaveText(/ago|just now/);

    // Toggle → absolute: the cell no longer reads relative, the button is active.
    await timeMode(page).click();
    await expect(timeMode(page)).toHaveClass(/\bactive\b/);
    await expect(timeCell(page)).not.toHaveText(/ago|just now/);
    await expect(timeCell(page)).toHaveText(/\d/);

    // Toggle back → relative again.
    await timeMode(page).click();
    await expect(timeMode(page)).not.toHaveClass(/\bactive\b/);
    await expect(timeCell(page)).toHaveText(/ago|just now/);
  });

  test('the toggle choice persists across reload, both directions', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);
    await expect(successRow(page)).toHaveCount(1);
    await expect(timeMode(page)).not.toHaveClass(/\bactive\b/);

    // Switch to absolute and reload: absolute survives (localStorage timeMode).
    await timeMode(page).click();
    await expect(timeMode(page)).toHaveClass(/\bactive\b/);
    await page.reload();
    await expect(successRow(page)).toHaveCount(1);
    await expect(timeMode(page)).toHaveClass(/\bactive\b/);
    await expect(timeCell(page)).not.toHaveText(/ago|just now/);

    // Switch back to relative and reload: relative survives.
    await timeMode(page).click();
    await expect(timeMode(page)).not.toHaveClass(/\bactive\b/);
    await page.reload();
    await expect(successRow(page)).toHaveCount(1);
    await expect(timeMode(page)).not.toHaveClass(/\bactive\b/);
    await expect(timeCell(page)).toHaveText(/ago|just now/);
  });
});
