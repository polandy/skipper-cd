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

// UA5 — Stack icon + monogram fallback. A stack with a resolvable icon renders an
// <img> in its stack-icon chip; a stack whose icon 404s falls back to a monogram
// chip (the stack's initial on an accent chip, no broken image). Both outcomes are
// produced deterministically in one instance: the harness commits an icon.svg
// override for `web` (resolves offline → 200) while `db` has none and the icon
// source_url is a dead address (auto-match → 404 → monogram).
test.describe('UA5: stack icon + monogram fallback', () => {
  test.use({
    startOptions: {
      stacks: ['web', 'db'],
      stackIcons: {
        web: '<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16"><rect width="16" height="16" fill="#4c9"/></svg>',
      },
    },
  });

  const stackIcon = (page: import('@playwright/test').Page, stack: string) =>
    page.locator(`[data-testid="deploy-row"][data-stack="${stack}"] [data-testid="stack-icon"]`);

  test('a resolvable icon renders an image; a 404 falls back to the monogram', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);

    // web: a real image chip — not a monogram — and it actually loaded, so it is
    // not a broken image.
    const web = stackIcon(page, 'web');
    await expect(web).toHaveCount(1);
    await expect(web).not.toHaveClass(/\bmono\b/);
    const img = web.locator('img');
    await expect(img).toHaveCount(1);
    await expect
      .poll(() => img.evaluate((el) => (el as HTMLImageElement).naturalWidth))
      .toBeGreaterThan(0);

    // db: nothing resolves → monogram chip with the stack's initial and no img.
    const db = stackIcon(page, 'db');
    await expect(db).toHaveClass(/\bmono\b/);
    await expect(db).toHaveText('d');
    await expect(db.locator('img')).toHaveCount(0);
  });
});

// UA6 — Icon refresh. The header refresh button and the `i` hotkey both clear the
// server icon cache (POST /api/icons/refresh) and reload every visible icon with a
// cache-busting `?v=<ts>` query so renamed or newly published icons appear. The
// test observes the network directly: the POST fires and the follow-up icon
// request carries the `?v=` param. The button also gets a `spinning` class.
test.describe('UA6: icon refresh', () => {
  test.use({
    startOptions: {
      stacks: ['web'],
      stackIcons: {
        web: '<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16"><rect width="16" height="16" fill="#4c9"/></svg>',
      },
    },
  });

  const iconChip = (page: import('@playwright/test').Page) =>
    page.locator('[data-testid="deploy-row"][data-stack="web"] [data-testid="stack-icon"]');

  // Arms request waiters, runs `act`, and asserts it drove a refresh POST and a
  // cache-busted icon reload. The initial icon load (iconVer 0) has no `?v=`, so
  // the busted request is unambiguously the reload.
  const expectRefresh = async (page: import('@playwright/test').Page, act: () => Promise<void>) => {
    const post = page.waitForRequest(
      (r) => r.url().endsWith('/api/icons/refresh') && r.method() === 'POST',
    );
    const busted = page.waitForRequest((r) => /\/api\/icons\/web\?v=\d+$/.test(r.url()));
    await act();
    await Promise.all([post, busted]);
  };

  test('the refresh button POSTs /api/icons/refresh and reloads icons cache-busted', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);
    await expect(iconChip(page).locator('img')).toHaveCount(1); // initial load settled

    const btn = page.locator('[data-testid="icon-refresh"]');
    await expectRefresh(page, () => btn.click());
    await expect(btn).toHaveClass(/\bspinning\b/);
  });

  test('the "i" hotkey does the same', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);
    await expect(iconChip(page).locator('img')).toHaveCount(1);

    await expectRefresh(page, () => page.keyboard.press('i'));
  });
});

// UA7 — Files pill. A deploy that changed files renders a `files-pill` in its row;
// clicking it inserts a `files-panel` (the changed file paths) right below the row,
// and clicking again removes it. The startup deploy is a stack's first deploy, so it
// has no previous commit to diff against (no `has_diffs`) — clicking yields the plain
// files-panel, not the diff-panel (that path is UA8). The startup success row already
// carries changed files, so no webhook is needed.
test.describe('UA7: files pill', () => {
  const successRow = (page: import('@playwright/test').Page) =>
    page.locator('[data-testid="deploy-row"][data-stack="web"][data-status="success"]');
  const filesPill = (page: import('@playwright/test').Page) =>
    successRow(page).locator('[data-testid="files-pill"]');
  const filesPanel = (page: import('@playwright/test').Page) =>
    page.locator('[data-testid="files-panel"]');

  test('clicking the files pill toggles the files panel below the row', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);
    await expect(successRow(page)).toHaveCount(1); // startup settled

    // The row carries a files pill; no panel is shown until it is clicked.
    await expect(filesPill(page)).toHaveCount(1);
    await expect(filesPanel(page)).toHaveCount(0);

    // First click inserts the panel directly after the row.
    await filesPill(page).click();
    await expect(filesPanel(page)).toHaveCount(1);
    await expect(filesPanel(page)).toBeVisible();
    // It lists at least one changed file path and sits immediately below the row.
    await expect(filesPanel(page).locator('.file-path').first()).toBeVisible();
    const siblingTestid = await successRow(page).evaluate(
      (row) => row.nextElementSibling?.getAttribute('data-testid') ?? null,
    );
    expect(siblingTestid).toBe('files-panel');

    // Second click removes it again.
    await filesPill(page).click();
    await expect(filesPanel(page)).toHaveCount(0);
  });
});

// UA8 — Diff panel. When a deploy has a previous commit to diff against, its event
// carries `has_diffs` and clicking the files-pill fetches `/api/events/{id}/diffs`
// and renders a `diff-panel` (not the plain files-panel) with per-line add/delete/
// hunk colouring. The startup deploy is a stack's first (no prior commit, no diffs);
// a second deploy driven by a webhook that bumps the compose image is the first with
// a real diff, so its success row is the one carrying `has_diffs`.
test.describe('UA8: diff panel', () => {
  const diffRow = (page: import('@playwright/test').Page) =>
    page.locator(
      '[data-testid="deploy-row"][data-stack="web"][data-status="success"][data-has-diffs="1"]',
    );
  const filesPill = (page: import('@playwright/test').Page) =>
    diffRow(page).locator('[data-testid="files-pill"]');
  const diffPanel = (page: import('@playwright/test').Page) =>
    page.locator('[data-testid="diff-panel"]');

  test('clicking the pill fetches diffs and renders a coloured diff panel', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);
    // The startup deploy settled but has no prior commit, so no diffs yet.
    await expect(diffRow(page)).toHaveCount(0);

    // A second deploy bumps the image → a real diff against the startup commit.
    skipper.setStackImage('web', '1.26');
    expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);

    // Its success row is flagged has_diffs and carries a files pill; nothing is
    // expanded until the pill is clicked.
    await expect(diffRow(page)).toHaveCount(1);
    await expect(filesPill(page)).toHaveCount(1);
    await expect(diffPanel(page)).toHaveCount(0);

    // Clicking the pill issues the diffs fetch and expands a diff-panel — not the
    // plain files-panel — directly below the row.
    const diffsReq = page.waitForRequest((r) => /\/api\/events\/[^/]+\/diffs$/.test(r.url()));
    await filesPill(page).click();
    await diffsReq;

    await expect(diffPanel(page)).toHaveCount(1);
    await expect(diffPanel(page)).toBeVisible();
    await expect(page.locator('[data-testid="files-panel"]')).toHaveCount(0);
    const siblingTestid = await diffRow(page).evaluate(
      (row) => row.nextElementSibling?.getAttribute('data-testid') ?? null,
    );
    expect(siblingTestid).toBe('diff-panel');

    // The image bump renders as a deleted and an added line (colour-classified),
    // plus a hunk header — the diff colouring the panel exists to show.
    await expect(diffPanel(page).locator('.diff-line.diff-del')).toContainText('nginx:1.25');
    await expect(diffPanel(page).locator('.diff-line.diff-add')).toContainText('nginx:1.26');
    await expect(diffPanel(page).locator('.diff-line.diff-hunk').first()).toBeVisible();

    // Clicking again collapses it.
    await filesPill(page).click();
    await expect(diffPanel(page)).toHaveCount(0);
  });
});
