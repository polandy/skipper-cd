import { test, expect } from '../fixtures/test';
import type { Page } from '@playwright/test';

// Maske B: Logs-View. See docs/e2e-tests.md §4.3.

// Distinctive stub-docker stdout line used by UB5 to prove child-process output
// is captured and rendered as a cmd-prefix line. No level word, no leading '[',
// so it can't be mistaken for a structured/level line.
const CHILD_LINE = 'container web-app-1 started';

// The log sort/follow toggles now live in the logs view-options popover (opened
// from the active logs view button), not the header row. This switches to the
// logs view if needed and opens its options popover so those toggles are
// actionable. Robust to the current view/popover state (e.g. after a reload).
async function openLogsOptions(page: Page): Promise<void> {
  const logsBtn = page.locator('[data-testid="view-toggle"] button[data-view="logs"]');
  const opts = page.locator('[data-testid="view-options"]');
  if (!(await logsBtn.evaluate((b) => b.classList.contains('active')))) {
    await logsBtn.click(); // switch deploys → logs
  }
  if (!(await opts.evaluate((el) => el.classList.contains('open')))) {
    await logsBtn.click(); // clicking the already-active button opens the popover
  }
}

// UB1 — View toggle. The deploys↔logs toggle switches which pane is visible and
// persists the choice in `localStorage.activeView`, so a reload restores the last
// view. Asserting the panes' visibility (not the button's active class) proves the
// toggle actually swaps the rendered surface, and the reloads prove persistence.
test.describe('UB1: view toggle', () => {
  const deploysBtn = (page: Page) =>
    page.locator('[data-testid="view-toggle"] button[data-view="deploys"]');
  const logsBtn = (page: Page) =>
    page.locator('[data-testid="view-toggle"] button[data-view="logs"]');
  const deployTable = (page: Page) => page.locator('#deploy-table');
  const logView = (page: Page) => page.locator('#log-view');

  test('switches deploys↔logs and persists across reload', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);

    // Default view is deploys: the (populated) table shows, the log pane is hidden.
    await expect(deployTable(page)).toBeVisible();
    await expect(logView(page)).toBeHidden();

    // Switch to logs: the pane appears, the table is hidden, choice persisted.
    await logsBtn(page).click();
    await expect(logView(page)).toBeVisible();
    await expect(deployTable(page)).toBeHidden();
    expect(await page.evaluate(() => localStorage.getItem('activeView'))).toBe('logs');

    // A reload restores the logs view from localStorage.
    await page.reload();
    await expect(logView(page)).toBeVisible();
    await expect(deployTable(page)).toBeHidden();

    // Switch back to deploys: the table returns, the pane hides, choice persisted.
    await deploysBtn(page).click();
    await expect(deployTable(page)).toBeVisible();
    await expect(logView(page)).toBeHidden();
    expect(await page.evaluate(() => localStorage.getItem('activeView'))).toBe('deploys');

    // And a reload restores deploys.
    await page.reload();
    await expect(deployTable(page)).toBeVisible();
    await expect(logView(page)).toBeHidden();
  });
});

// UB2 — Log lines + level badges. Real backend log output is replayed over the
// /api/logs SSE stream and rendered as `log-line`s, each carrying its slog level
// as `data-level` plus a matching `level-badge`. A failing startup deploy emits
// INFO ("deploying stack") and ERROR ("docker compose up failed…") lines, and a
// webhook with a bad signature adds a WARN ("webhook rejected: invalid
// signature") — a deterministic INFO/WARN/ERROR mix produced by the real backend.
// DEBUG is intentionally out of scope: the default slog handler filters below
// INFO and skipper exposes no log-level toggle, so a DEBUG line can never reach
// the ring through the real binary.
test.describe('UB2: log lines + level badges', () => {
  test.use({
    startOptions: {
      stacks: ['web'],
      stubEnv: { STUB_DOCKER_FAIL_ON: 'up' },
      readiness: 'listening',
    },
  });

  const lineAtLevel = (page: Page, level: string) =>
    page.locator(`[data-testid="log-line"][data-level="${level}"]`);

  test('renders replayed lines with the correct level badge per level', async ({ page, skipper }) => {
    // The rejected webhook yields the WARN line; INFO + ERROR come from the
    // failed startup deploy already captured in the ring.
    expect(await skipper.sendBadWebhook('refs/heads/main')).toBe(401);

    await page.goto(`${skipper.baseURL}/`);
    await page.locator('[data-testid="view-toggle"] button[data-view="logs"]').click();

    // Each level renders at least one line whose badge text equals the level —
    // proving the level → badge mapping end-to-end against real log output.
    for (const level of ['INFO', 'WARN', 'ERROR']) {
      const line = lineAtLevel(page, level).first();
      await expect(line).toBeVisible();
      await expect(line.locator('[data-testid="level-badge"]')).toHaveText(level);
    }
  });
});

// UB3 — Sort toggle. The log pane defaults to newest-first (descending); the
// `log-sort` toggle flips the rendered order to oldest-first and persists the
// choice in `localStorage.logSort`, so a reload keeps the chosen order. We
// fingerprint the rendered line order (the DOM sequence of every `log-line`)
// and assert the toggle reverses it exactly, then that a reload preserves it —
// selecting lines only by `data-testid` and treating localStorage as the
// persisted source of truth rather than probing the button's active class.
test.describe('UB3: sort toggle', () => {
  test.use({
    startOptions: {
      stacks: ['web'],
      stubEnv: { STUB_DOCKER_FAIL_ON: 'up' },
      readiness: 'listening',
    },
  });

  const sortBtn = (page: Page) => page.locator('[data-testid="log-sort"]');
  const logLines = (page: Page) => page.locator('[data-testid="log-line"]');
  const lineOrder = (page: Page) => logLines(page).allTextContents();
  const logSort = (page: Page) =>
    page.evaluate(() => localStorage.getItem('logSort'));

  test('flips newest↔oldest order and persists across reload', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);
    await openLogsOptions(page);

    // Wait for the replayed backlog to settle (the terminal ERROR line is last).
    await expect(page.locator('[data-testid="log-line"][data-level="ERROR"]').first()).toBeVisible();
    const lineCount = await logLines(page).count();
    expect(lineCount).toBeGreaterThan(1);

    // Default order is newest-first, and no explicit choice is stored yet.
    expect(await logSort(page)).toBeNull();
    const descOrder = await lineOrder(page);

    // Toggling flips to oldest-first: the rendered order is the exact reverse,
    // and the choice is persisted as ascending.
    await sortBtn(page).click();
    await expect(logLines(page)).toHaveCount(lineCount);
    const ascOrder = await lineOrder(page);
    expect(ascOrder).toEqual([...descOrder].reverse());
    expect(await logSort(page)).toBe('asc');

    // A reload restores the ascending order from localStorage.
    await page.reload();
    await openLogsOptions(page);
    await expect(logLines(page)).toHaveCount(lineCount);
    expect(await lineOrder(page)).toEqual(ascOrder);
    expect(await logSort(page)).toBe('asc');

    // Toggling back returns to the original newest-first order.
    await sortBtn(page).click();
    await expect(logLines(page)).toHaveCount(lineCount);
    expect(await lineOrder(page)).toEqual(descOrder);
    expect(await logSort(page)).toBe('desc');
  });
});

// UB4 — Follow toggle. Following (the default) pins the pane to the newest edge
// whenever a fresh line streams in; unfollowing leaves the scroll position
// alone. The choice persists in `localStorage.followLogs`. We fill the ring so
// the pane overflows, scroll away from the newest edge (the top, in the default
// descending order), then drive one live line via a bad-signature webhook and
// assert the pane snaps back to `scrollTop === 0` only while following.
test.describe('UB4: follow toggle', () => {
  test.use({
    startOptions: {
      stacks: ['web'],
      stubEnv: { STUB_DOCKER_FAIL_ON: 'up' },
      readiness: 'listening',
    },
  });

  const logLines = (page: Page) => page.locator('[data-testid="log-line"]');
  const followBtn = (page: Page) => page.locator('[data-testid="follow-logs"]');
  const followLogs = (page: Page) =>
    page.evaluate(() => localStorage.getItem('followLogs'));
  const scrollTop = (page: Page) =>
    page.evaluate(() => document.getElementById('log-pane')!.scrollTop);
  // Scroll to the oldest edge (the bottom in descending order) so the newest
  // edge (top) is out of view — any autoscroll is then observable as scrollTop→0.
  const scrollAwayFromNewest = (page: Page) =>
    page.evaluate(() => {
      const p = document.getElementById('log-pane')!;
      p.scrollTop = p.scrollHeight;
    });

  test('autoscrolls to the newest edge only while following, and persists', async ({ page, skipper }) => {
    // Fill the ring so the log pane overflows and becomes scrollable. Each
    // bad-signature webhook adds one live WARN line.
    for (let i = 0; i < 60; i++) {
      expect(await skipper.sendBadWebhook('refs/heads/main')).toBe(401);
    }

    await page.goto(`${skipper.baseURL}/`);
    await openLogsOptions(page);
    await expect(logLines(page).first()).toBeVisible();

    // Precondition: the pane actually overflows, so scrolling is meaningful.
    const overflows = await page.evaluate(() => {
      const p = document.getElementById('log-pane')!;
      return p.scrollHeight > p.clientHeight;
    });
    expect(overflows).toBe(true);

    // Following is on by default (no explicit choice stored yet).
    expect(await followLogs(page)).toBeNull();

    // Scroll away from the newest edge, then a fresh line streams in: following
    // snaps the pane back to the newest edge (scrollTop 0 in descending order).
    await scrollAwayFromNewest(page);
    expect(await scrollTop(page)).toBeGreaterThan(0);
    let count = await logLines(page).count();
    expect(await skipper.sendBadWebhook('refs/heads/main')).toBe(401);
    await expect(logLines(page)).toHaveCount(count + 1);
    expect(await scrollTop(page)).toBe(0);

    // Turn following off; the choice is persisted.
    await followBtn(page).click();
    expect(await followLogs(page)).toBe('false');

    // Scroll away again: now a fresh line must NOT drag the pane to the newest
    // edge — the reading position is left where the user put it.
    await scrollAwayFromNewest(page);
    expect(await scrollTop(page)).toBeGreaterThan(0);
    count = await logLines(page).count();
    expect(await skipper.sendBadWebhook('refs/heads/main')).toBe(401);
    await expect(logLines(page)).toHaveCount(count + 1);
    expect(await scrollTop(page)).toBeGreaterThan(0);

    // The unfollowed choice survives a reload.
    await page.reload();
    await openLogsOptions(page);
    expect(await followLogs(page)).toBe('false');
  });
});

// UB5 — Log-line prefixes. A structured deploy line carries a `stack` attr,
// rendered as a `stack-prefix` alongside its level badge. A child-process line
// (captured docker/git stdout+stderr) carries `cmd`+`stream` attrs instead and
// renders a muted `cmd-prefix` with NO level badge. Both kinds are produced by
// the real backend from a single failing startup deploy: `STUB_DOCKER_ECHO`
// makes the stub `docker` print a line on `up` (captured as a `cmd`/`stream`
// child line, cmd="docker"), and `STUB_DOCKER_FAIL_ON=up` makes that same up
// fail so the deploy emits its own `stack`-tagged INFO/ERROR lines. We select
// only by `data-testid` and assert the two prefix shapes end-to-end.
test.describe('UB5: log-line prefixes', () => {
  test.use({
    startOptions: {
      stacks: ['web'],
      stubEnv: { STUB_DOCKER_FAIL_ON: 'up', STUB_DOCKER_ECHO: CHILD_LINE },
      readiness: 'listening',
    },
  });

  // The docker child line renders with data-level="cmd" (not a slog level).
  const cmdLine = (page: Page) =>
    page.locator('[data-testid="log-line"][data-level="cmd"]').first();
  // A deploy line for the "web" stack — the terminal ERROR is stable.
  const stackLine = (page: Page) =>
    page.locator('[data-testid="log-line"][data-level="ERROR"]').first();

  test('renders a stack-prefix on deploy lines and a cmd-prefix (no badge) on child output', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);
    await page.locator('[data-testid="view-toggle"] button[data-view="logs"]').click();

    // Child-process output: the captured docker stdout renders a muted
    // [docker] cmd-prefix, carries the echoed message, and has NO level badge.
    const cmd = cmdLine(page);
    await expect(cmd).toBeVisible();
    await expect(cmd.locator('[data-testid="cmd-prefix"]')).toHaveText('[docker]');
    await expect(cmd).toContainText(CHILD_LINE);
    await expect(cmd.locator('[data-testid="level-badge"]')).toHaveCount(0);

    // Structured deploy line: a [web] stack-prefix next to a real level badge —
    // the two coexist, unlike the child line which has neither a stack nor a badge.
    const stack = stackLine(page);
    await expect(stack).toBeVisible();
    await expect(stack.locator('[data-testid="stack-prefix"]')).toHaveText('[web]');
    await expect(stack.locator('[data-testid="level-badge"]')).toHaveText('ERROR');
    await expect(stack.locator('[data-testid="cmd-prefix"]')).toHaveCount(0);
  });
});

// UB6 — Diff pill in the log view. A `deploy complete` line carries the deploy
// event's `event_id`, rendered as a `diff-pill`. Clicking it fetches
// `/api/events/{id}/diffs` and inserts the same `diff-panel` directly below the
// log line (clicking again collapses it) — the log-view twin of UA8's deploy-row
// diff. The startup deploy is the stack's first (no prior commit → empty diffs); a
// second deploy that bumps the compose image is the first with a real diff, so its
// `deploy complete` line — the newest, hence topmost under the default descending
// sort — is the one whose pill expands a populated panel. We drive the real backend
// (webhook → deploy → SSE) and select only by `data-testid`.
test.describe('UB6: diff pill in logs', () => {
  // Both deploy-complete lines carry a diff-pill; the newest (topmost) is the
  // second deploy, the only one with a real diff to show.
  const newestDiffPill = (page: Page) =>
    page.locator('[data-testid="log-line"] [data-testid="diff-pill"]').first();
  const diffPanel = (page: Page) => page.locator('[data-testid="diff-panel"]');

  test('clicking a deploy line pill expands the diff panel below it', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);
    await page.locator('[data-testid="view-toggle"] button[data-view="logs"]').click();

    // A second deploy bumps the image → its `deploy complete` line has a real diff.
    skipper.setStackImage('web', '1.26');
    expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);

    // Both deploys logged a `deploy complete` line, each with a diff-pill; wait for
    // both so `.first()` is deterministically the newest (the one with the diff).
    await expect(page.locator('[data-testid="diff-pill"]')).toHaveCount(2);
    await expect(diffPanel(page)).toHaveCount(0);

    // Clicking the pill issues the diffs fetch and expands a diff-panel as the log
    // line's own next sibling — not somewhere else in the pane.
    const diffsReq = page.waitForRequest((r) => /\/api\/events\/[^/]+\/diffs$/.test(r.url()));
    await newestDiffPill(page).click();
    await diffsReq;

    await expect(diffPanel(page)).toHaveCount(1);
    await expect(diffPanel(page)).toBeVisible();
    const siblingTestid = await newestDiffPill(page).evaluate(
      (pill) =>
        pill.closest('[data-testid="log-line"]')?.nextElementSibling?.getAttribute('data-testid') ??
        null,
    );
    expect(siblingTestid).toBe('diff-panel');

    // It is a populated diff (the image bump), proving it is the diff-panel and not
    // the plain "No diff recorded" note.
    await expect(diffPanel(page)).toContainText('nginx:1.26');

    // Clicking again collapses it.
    await newestDiffPill(page).click();
    await expect(diffPanel(page)).toHaveCount(0);
  });
});
