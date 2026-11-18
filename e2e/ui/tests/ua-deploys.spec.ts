import { test, expect } from '../fixtures/test';
import { visualSnapshot } from '../fixtures/snapshot';

// Maske A: Deploys-View. See docs/dev/e2e-tests.md §4.2.

// Dynamic cells that must be masked out of deploy-table screenshots so relative
// times and durations never diff (docs/dev/e2e-tests.md §5).
const deployMasks = (page: import('@playwright/test').Page) => [
  page.locator('[data-testid="time-cell"]'),
  page.locator('[data-testid="duration-cell"]'),
  page.locator('[data-testid="deploy-day"]'),
];

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

  // Snapshot: the settled deploys table (§5 anchor), dynamic cells masked.
  await visualSnapshot(page.locator('[data-testid="deploys-table"]'), 'deploys-table.png', {
    mask: deployMasks(page),
  });
});

// UA2 — Status badges. Each deploy status the UI renders is driven through the
// real backend (recipes in docs/dev/e2e-tests.md §3) and its row asserted to carry
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

  test.describe('rolled_back_unhealthy', () => {
    // Startup up#1 succeeds (sets LastDeployedCommit); the health-gated deploy
    // up#2 fails and the rollback up#3 — rerunning the same gate — fails too
    // → rolled_back_unhealthy. The badge stacks its label on two lines
    // ("rolled back" + "unhealthy" spans), hence the regex.
    test.use({
      startOptions: {
        stacks: ['web'],
        healthCheck: ['web'],
        stubEnv: { STUB_DOCKER_FAIL_NTH_UP: '2,3' },
      },
    });

    test('rolled_back_unhealthy', async ({ page, skipper }) => {
      await page.goto(`${skipper.baseURL}/`);
      await expect(webRow(page, 'success')).toHaveCount(1);

      skipper.setStackImage('web', '1.26');
      expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);

      const row = webRow(page, 'rolled_back_unhealthy');
      await expect(row).toHaveCount(1);
      await expect(row.locator('[data-testid="status-badge"]')).toHaveText(
        /rolled back\s*unhealthy/,
      );
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
      // Nothing opens by itself; a click opens the detail with the verdict.
      await expect(page.locator('[data-testid="detail-panel"][data-kind="deploy"]')).toHaveCount(0);
      await row.click();
      await expect(page.locator('[data-testid="detail-verdict"]')).toBeVisible();
    });
  });
});

// UA3 — Skipped deploys are never shown. An unchanged stack emits a `skipped`
// event, but the UI drops it: no row is ever rendered, and there is no filter
// control to reveal one. The drop is proven by ordering — after the no-op
// webhook (the skip), a real change deploys to a second success row. SSE is
// ordered and deploys serialize on one mutex, so once that row appears the skip
// event was already delivered, yet no skipped row exists.
test('UA3: skipped deploys never render a row', async ({ page, skipper }) => {
  const webRow = (status: string) =>
    page.locator(`[data-testid="deploy-row"][data-stack="web"][data-status="${status}"]`);

  await page.goto(`${skipper.baseURL}/`);
  await expect(webRow('success')).toHaveCount(1); // startup settled

  // A webhook with no new commit leaves the stack unchanged → skipped.
  expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);

  // A real change then deploys to a second success row; its arrival proves the
  // earlier skip event was delivered and dropped rather than merely slow.
  skipper.setStackImage('web', '1.26');
  expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);
  await expect(webRow('success')).toHaveCount(2);

  // No skipped row was ever rendered, and there is no skip-filter control.
  await expect(webRow('skipped')).toHaveCount(0);
  await expect(page.locator('[data-testid="skip-filter"]')).toHaveCount(0);
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
  // time-mode now lives in the deploys view-options popover (not the header row).
  // Deploys is the default active view, so clicking its button opens the popover.
  const openDeploysOptions = async (page: import('@playwright/test').Page) => {
    const opts = page.locator('[data-testid="view-options"]');
    if (await opts.evaluate((el) => el.classList.contains('open'))) return;
    await page.locator('[data-testid="view-toggle"] button[data-view="deploys"]').click();
  };

  test('toggling switches Time cells relative ↔ absolute', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);
    await expect(successRow(page)).toHaveCount(1); // startup settled
    await openDeploysOptions(page);

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
    await openDeploysOptions(page);
    await expect(timeMode(page)).not.toHaveClass(/\bactive\b/);

    // Switch to absolute and reload: absolute survives (localStorage timeMode).
    await timeMode(page).click();
    await expect(timeMode(page)).toHaveClass(/\bactive\b/);
    await page.reload();
    await expect(successRow(page)).toHaveCount(1);
    await openDeploysOptions(page);
    await expect(timeMode(page)).toHaveClass(/\bactive\b/);
    await expect(timeCell(page)).not.toHaveText(/ago|just now/);

    // Switch back to relative and reload: relative survives.
    await timeMode(page).click();
    await expect(timeMode(page)).not.toHaveClass(/\bactive\b/);
    await page.reload();
    await expect(successRow(page)).toHaveCount(1);
    await openDeploysOptions(page);
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

// UA7 — Row opens its detail. Clicking a deploy row that changed files opens the
// row's deploy detail right below it, on the Changes tab listing the changed file
// paths (`files-panel`), and clicking again closes it. The startup deploy is a stack's first deploy, so it
// has no previous commit to diff against (no `has_diffs`) — clicking yields the plain
// files-panel, not the diff-panel (that path is UA8). The startup success row already
// carries changed files, so no webhook is needed.
test.describe('UA7: row detail', () => {
  const successRow = (page: import('@playwright/test').Page) =>
    page.locator('[data-testid="deploy-row"][data-stack="web"][data-status="success"]');
  const rowOpener = (page: import('@playwright/test').Page) =>
    successRow(page).locator('.stack-name');
  const filesPanel = (page: import('@playwright/test').Page) =>
    page.locator('[data-testid="files-panel"]');

  test('clicking the row toggles the deploy detail on its Changes tab', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);
    await expect(successRow(page)).toHaveCount(1); // startup settled

    // No panel is shown until the row is clicked.
    await expect(filesPanel(page)).toHaveCount(0);

    // First click opens the deploy detail directly after the row.
    await rowOpener(page).click();
    await expect(filesPanel(page)).toHaveCount(1);
    await expect(filesPanel(page)).toBeVisible();
    // It lists at least one changed file path and sits immediately below the row.
    await expect(filesPanel(page).locator('.file-path').first()).toBeVisible();
    const siblingTestid = await successRow(page).evaluate(
      (row) => row.nextElementSibling?.getAttribute('data-testid') ?? null,
    );
    expect(siblingTestid).toBe('detail-panel');
    await expect(page.locator('[data-testid="detail-tab"][aria-selected="true"]')).toHaveAttribute('data-tab', 'changes');

    // Second click removes it again.
    await rowOpener(page).click();
    await expect(filesPanel(page)).toHaveCount(0);
  });
});

// UA8 — Diff panel. When a deploy has a previous commit to diff against, its event
// carries `has_diffs` and clicking its row fetches `/api/events/{id}/diffs`
// and renders a `diff-panel` (not the plain files-panel) in the deploy detail's
// Changes tab with per-line add/delete/
// hunk colouring. The startup deploy is a stack's first (no prior commit, no diffs);
// a second deploy driven by a webhook that bumps the compose image is the first with
// a real diff, so its success row is the one carrying `has_diffs`.
test.describe('UA8: diff panel', () => {
  const diffRow = (page: import('@playwright/test').Page) =>
    page.locator(
      '[data-testid="deploy-row"][data-stack="web"][data-status="success"][data-has-diffs="1"]',
    );
  const rowOpener = (page: import('@playwright/test').Page) =>
    diffRow(page).locator('.stack-name');
  const diffPanel = (page: import('@playwright/test').Page) =>
    page.locator('[data-testid="diff-panel"]');

  test('clicking the row fetches diffs and renders a coloured diff panel', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);
    // The startup deploy settled but has no prior commit, so no diffs yet.
    await expect(diffRow(page)).toHaveCount(0);

    // A second deploy bumps the image → a real diff against the startup commit.
    skipper.setStackImage('web', '1.26');
    expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);

    // Its success row is flagged has_diffs; nothing is expanded until the row
    // is clicked.
    await expect(diffRow(page)).toHaveCount(1);
    await expect(rowOpener(page)).toHaveCount(1);
    await expect(diffPanel(page)).toHaveCount(0);

    // Clicking the row issues the diffs fetch and opens the deploy detail below
    // the row, its Changes tab holding a diff-panel — not the plain files-panel.
    const diffsReq = page.waitForRequest((r) => /\/api\/events\/[^/]+\/diffs$/.test(r.url()));
    await rowOpener(page).click();
    await diffsReq;

    await expect(diffPanel(page)).toHaveCount(1);
    await expect(diffPanel(page)).toBeVisible();
    await expect(page.locator('[data-testid="files-panel"]')).toHaveCount(0);
    const siblingTestid = await diffRow(page).evaluate(
      (row) => row.nextElementSibling?.getAttribute('data-testid') ?? null,
    );
    expect(siblingTestid).toBe('detail-panel');
    await expect(page.locator('[data-testid="detail-panel"] [data-testid="diff-panel"]')).toHaveCount(1);

    // The image bump renders as a deleted and an added line (colour-classified),
    // plus a hunk header — the diff colouring the panel exists to show.
    await expect(diffPanel(page).locator('.diff-line.diff-del')).toContainText('nginx:1.25');
    await expect(diffPanel(page).locator('.diff-line.diff-add')).toContainText('nginx:1.26');
    await expect(diffPanel(page).locator('.diff-line.diff-hunk').first()).toBeVisible();
    // No git header lines, and lines are single-height (no blank line between).
    await expect(diffPanel(page).locator('.diff-line.diff-meta')).toHaveCount(0);
    const lineHeights = await diffPanel(page)
      .locator('.diff-line')
      .evaluateAll((els) => els.map((e) => e.getBoundingClientRect().height));
    for (const h of lineHeights) expect(h).toBeLessThan(24);

    // Clicking again collapses it.
    await rowOpener(page).click();
    await expect(diffPanel(page)).toHaveCount(0);
  });
});

// UA9 — Error detail. A `failed` deploy's detail leads with the cause: its
// verdict sits above the facts and tabs, and the Output tab carries the real
// backend error. Nothing opens by itself — no error box rides the list. The
// startup `up` fails with no prior commit to roll back to, so the stack's first
// row is `failed`.
test.describe('UA9: error detail', () => {
  test.use({
    startOptions: { stacks: ['web'], stubEnv: { STUB_DOCKER_FAIL_ON: 'up' }, readiness: 'listening' },
  });

  const failedRow = (page: import('@playwright/test').Page) =>
    page.locator('[data-testid="deploy-row"][data-stack="web"][data-status="failed"]');
  const panel = (page: import('@playwright/test').Page) =>
    page.locator('[data-testid="detail-panel"][data-kind="deploy"]');

  test('a failed deploy’s detail shows the cause before the diff and the output', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);

    await expect(failedRow(page)).toHaveCount(1);
    await expect(panel(page)).toHaveCount(0);
    await failedRow(page).click();

    // The detail belongs to the failed row: it is the row's next sibling.
    const siblingTestid = await failedRow(page).evaluate(
      (row) => row.nextElementSibling?.getAttribute('data-testid') ?? null,
    );
    expect(siblingTestid).toBe('detail-panel');
    await expect(panel(page)).toHaveAttribute('data-stack', 'web');

    // The verdict names the failed phase and comes before the facts and tabs.
    const order = await panel(page).evaluate((p) =>
      Array.from(p.children).map((c) => c.getAttribute('data-testid') ?? c.className),
    );
    expect(order.indexOf('detail-verdict')).toBeLessThan(order.indexOf('detail-facts'));
    await expect(panel(page).locator('[data-testid="detail-verdict"]')).toContainText('Failed at docker compose up');

    // The Output tab carries the real backend error.
    await panel(page).locator('[data-testid="detail-tab"][data-tab="output"]').click();
    await expect(panel(page).locator('[data-testid="deploy-output"]')).toContainText('docker compose up');
  });
});

// UA10 — Empty state. A stack-free instance never deploys, so no event is ever
// emitted: once the (empty) history replays and the `synced` marker lands, the
// skeleton (T4.17) yields to the genuine-empty `empty-state` and the deploy
// table stays hidden with zero rows. Its persistence *after the SSE stream
// connects* proves the empty path — not a mid-connect flash. The skeleton→empty
// transition itself is covered by Maske AH (UAH1).
test.describe('UA10: empty state', () => {
  test.use({ startOptions: { stacks: [] } });

  test('a UI with no deploy events shows the empty-state placeholder and no table', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);

    const emptyState = page.locator('[data-testid="empty-state"]');
    await expect(emptyState).toBeVisible();

    // Wait until the SSE stream is open and its history has replayed; an event
    // would have called showTable() by now, so the placeholder standing is proof.
    await expect(page.locator('[data-state="connected"]')).toHaveCount(1);

    await expect(emptyState).toBeVisible();
    await expect(page.locator('[data-testid="deploy-row"]')).toHaveCount(0);
    await expect(page.locator('#deploy-table')).toBeHidden();
  });
});

// UA11 — Queued row replaced on resume. Pausing autosync then pushing a change
// defers the stack to a `queued` (paused) row. Re-enabling autosync drains the
// queue and the stack deploys — the stale queued row must be *replaced*, not
// left lingering beside the new deploy row. (Regression: only deploying→terminal
// transitions were de-duplicated, so the queued row survived the resume.)
test('UA11: the queued row is replaced when autosync resumes', async ({ page, skipper }) => {
  const webRow = (status: string) =>
    page.locator(`[data-testid="deploy-row"][data-stack="web"][data-status="${status}"]`);
  const anyWebRow = () => page.locator('[data-testid="deploy-row"][data-stack="web"]');

  await page.goto(`${skipper.baseURL}/`);
  await expect(webRow('success')).toHaveCount(1); // startup settled

  // Pause globally, then push a change → the stack is deferred to a queued row.
  expect(await skipper.postAutosync('', false)).toBe(200);
  skipper.setStackImage('web', '1.26');
  expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);
  await expect(webRow('queued')).toHaveCount(1);
  await expect(anyWebRow()).toHaveCount(2); // startup success + queued

  // Resume: the queue drains and the stack deploys to a fresh success row. The
  // stale queued row is gone — not left standing beside the new row.
  expect(await skipper.postAutosync('', true)).toBe(200);
  await expect(webRow('success')).toHaveCount(2);
  await expect(webRow('queued')).toHaveCount(0);
  await expect(anyWebRow()).toHaveCount(2);
});

// UA12 — Queued row shows the pending diff. A change deferred while autosync is
// paused emits a `queued` event that must still carry the diff of what is
// waiting: its paused row is flagged `has_diffs` and clicking the row
// expands the coloured diff-panel — not the plain file-path list. (Regression:
// queued events were emitted with nil diffs, so the pending row could only show
// file paths, never the effective diff.)
test.describe('UA12: queued row diff', () => {
  const queuedRow = (page: import('@playwright/test').Page) =>
    page.locator('[data-testid="deploy-row"][data-stack="web"][data-status="queued"]');
  const rowOpener = (page: import('@playwright/test').Page) =>
    queuedRow(page).locator('.stack-name');
  const diffPanel = (page: import('@playwright/test').Page) =>
    page.locator('[data-testid="diff-panel"]');

  test('a paused stack’s queued row expands the pending diff', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);
    // The startup deploy settled and set LastDeployedCommit, so a later change
    // has a real diff to show.
    await expect(
      page.locator('[data-testid="deploy-row"][data-stack="web"][data-status="success"]'),
    ).toHaveCount(1);

    // Pause, then push an image bump → the stack is deferred to a queued row that
    // carries the diff against the startup commit.
    expect(await skipper.postAutosync('', false)).toBe(200);
    skipper.setStackImage('web', '1.26');
    expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);

    await expect(queuedRow(page)).toHaveCount(1);
    await expect(queuedRow(page)).toHaveAttribute('data-has-diffs', '1');
    await expect(rowOpener(page)).toHaveCount(1);
    await expect(diffPanel(page)).toHaveCount(0);

    // Clicking the row fetches diffs and opens the detail's Changes tab: a
    // diff-panel (not the plain files-panel), the image change colour-classified.
    const diffsReq = page.waitForRequest((r) => /\/api\/events\/[^/]+\/diffs$/.test(r.url()));
    await rowOpener(page).click();
    await diffsReq;

    await expect(diffPanel(page)).toHaveCount(1);
    await expect(diffPanel(page)).toBeVisible();
    await expect(page.locator('[data-testid="files-panel"]')).toHaveCount(0);
    await expect(diffPanel(page).locator('.diff-line.diff-del')).toContainText('nginx:1.25');
    await expect(diffPanel(page).locator('.diff-line.diff-add')).toContainText('nginx:1.26');
  });
});

// UA13 — The deploy detail's tab strip follows the ARIA tabs pattern, like the
// Stacks detail (UBC9): the arrow keys move selection and focus, and the pane
// is labelled by the selected tab. A failed deploy offers Changes and Output.
test.describe('UA13: deploy detail tabs by keyboard', () => {
  test.use({
    startOptions: { stacks: ['web'], stubEnv: { STUB_DOCKER_FAIL_ON: 'up' }, readiness: 'listening' },
  });

  test('the arrow keys switch the deploy detail tabs', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);
    await page.locator('[data-testid="deploy-row"][data-stack="web"][data-status="failed"]').click();
    const p = page.locator('[data-testid="detail-panel"][data-kind="deploy"]');
    const tabs = p.locator('[data-testid="detail-tab"]');
    await expect(tabs.first()).toHaveAttribute('aria-selected', 'true');
    const count = await tabs.count();
    expect(count).toBeGreaterThan(1);
    const last = tabs.nth(count - 1);
    const lastId = (await last.getAttribute('data-tab'))!;

    await tabs.first().focus();
    await page.keyboard.press('End');
    await expect(last).toBeFocused();
    await expect(last).toHaveAttribute('aria-selected', 'true');
    await expect(p).toHaveAttribute('data-tab', lastId);
    await expect(p.locator('[role="tabpanel"]')).toHaveAttribute('aria-labelledby', `deploy-detail-tab-${lastId}`);
    await page.keyboard.press('ArrowRight'); // wraps to the first tab
    await expect(tabs.first()).toBeFocused();
    await expect(tabs.first()).toHaveAttribute('aria-selected', 'true');
  });
});

// UA14 — a deploy row opens by keyboard: its stack name is the row's toggle
// (Enter opens, aria-expanded follows), Escape from inside the detail closes it
// and hands the focus back to the toggle.
test.describe('UA14: deploy row detail by keyboard', () => {
  test.use({
    startOptions: { stacks: ['web'], stubEnv: { STUB_DOCKER_FAIL_ON: 'up' }, readiness: 'listening' },
  });

  test('Enter opens the deploy detail and Escape closes it', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);
    const row = page.locator('[data-testid="deploy-row"][data-stack="web"][data-status="failed"]');
    const toggle = row.locator('.row-toggle');
    const p = page.locator('[data-testid="detail-panel"][data-kind="deploy"]');
    await expect(toggle).toHaveAttribute('role', 'button');
    await expect(toggle).toHaveAttribute('aria-expanded', 'false');

    await toggle.focus();
    await page.keyboard.press('Enter');
    await expect(p).toBeVisible();
    await expect(toggle).toHaveAttribute('aria-expanded', 'true');

    await p.locator('[data-testid="detail-tab"]').first().focus();
    await page.keyboard.press('Escape');
    await expect(p).toHaveCount(0);
    await expect(toggle).toHaveAttribute('aria-expanded', 'false');
    await expect(toggle).toBeFocused();

    await page.keyboard.press('Enter');
    await expect(p).toBeVisible();
    await page.keyboard.press('Escape');
    await expect(p).toHaveCount(0);
    await expect(toggle).toHaveAttribute('aria-expanded', 'false');
  });
});
