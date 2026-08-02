import { test, expect } from '../fixtures/test';
import type { Page } from '@playwright/test';

// Maske B: Logs-View. See dev-docs/e2e-tests.md §4.3.

// Distinctive stub-docker stdout line used by UB5 to prove child-process output
// is captured and rendered as a cmd-prefix line. No level word, no leading '[',
// so it can't be mistaken for a structured/level line.
const CHILD_LINE = 'container web-app-1 started';

// Switches to the Logs view if it isn't already active. Its controls (follow,
// wrap, search, fullscreen) live inline in the panel's own header (see UB8/UB9)
// rather than behind a popover, so there is no options step to open.
async function openLogsView(page: Page): Promise<void> {
  const logsBtn = page.locator('[data-testid="view-toggle"] button[data-view="logs"]');
  if (!(await logsBtn.evaluate((b) => b.classList.contains('active')))) {
    await logsBtn.click();
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
  // The newest line sits at the *bottom* (chronological order), so "at the
  // tail" means scrolled to the end, not to zero.
  const atTail = (page: Page) =>
    page.evaluate(() => {
      const p = document.getElementById('log-pane')!;
      return p.scrollTop + p.clientHeight >= p.scrollHeight - 40;
    });
  // Scroll to the oldest edge (the top) so the newest edge is out of view.
  const scrollAwayFromNewest = (page: Page) =>
    page.evaluate(() => {
      document.getElementById('log-pane')!.scrollTop = 0;
    });
  const newPill = (page: Page) => page.locator('[data-testid="log-newpill"]');

  test('autoscrolls to the newest edge only while following, and persists', async ({ page, skipper }) => {
    // Fill the ring so the log pane overflows and becomes scrollable. Each
    // bad-signature webhook adds one live WARN line.
    for (let i = 0; i < 60; i++) {
      expect(await skipper.sendBadWebhook('refs/heads/main')).toBe(401);
    }

    await page.goto(`${skipper.baseURL}/`);
    await openLogsView(page);
    await expect(logLines(page).first()).toBeVisible();

    // Precondition: the pane actually overflows, so scrolling is meaningful.
    const overflows = await page.evaluate(() => {
      const p = document.getElementById('log-pane')!;
      return p.scrollHeight > p.clientHeight;
    });
    expect(overflows).toBe(true);

    // Following is on by default (no explicit choice stored yet), and the pane
    // sits at the tail.
    expect(await followLogs(page)).toBeNull();
    expect(await atTail(page)).toBe(true);

    // Scrolling away from the tail disengages following by itself — the control
    // reports where the pane actually is instead of having to be toggled. A
    // fresh line then leaves the reading position alone and is announced by the
    // "N new lines" pill instead.
    await scrollAwayFromNewest(page);
    await expect(followBtn(page)).not.toHaveClass(/\bon\b/);
    expect(await followLogs(page)).toBe('false');
    const parked = await scrollTop(page);
    let count = await logLines(page).count();
    expect(await skipper.sendBadWebhook('refs/heads/main')).toBe(401);
    await expect(logLines(page)).toHaveCount(count + 1);
    expect(await scrollTop(page)).toBe(parked);
    await expect(newPill(page)).toBeVisible();

    // Clicking the pill re-engages following and jumps to the tail, which also
    // settles the pill.
    await newPill(page).click();
    expect(await atTail(page)).toBe(true);
    expect(await followLogs(page)).toBe('true');
    await expect(newPill(page)).toBeHidden();

    // While following, a fresh line keeps the pane pinned to the tail.
    count = await logLines(page).count();
    expect(await skipper.sendBadWebhook('refs/heads/main')).toBe(401);
    await expect(logLines(page)).toHaveCount(count + 1);
    expect(await atTail(page)).toBe(true);

    // Turning it off by hand persists the choice too.
    await followBtn(page).click();
    expect(await followLogs(page)).toBe('false');

    // The unfollowed choice survives a reload.
    await page.reload();
    await openLogsView(page);
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
  // Selected by the echoed text, not by position: git's own clone/fetch output
  // is a cmd line too, and which one comes first is a property of the run
  // order, not of what this test is about.
  const cmdLine = (page: Page) =>
    page.locator('[data-testid="log-line"][data-level="cmd"]', { hasText: CHILD_LINE }).first();
  // A deploy line for the "web" stack — the terminal ERROR is stable. Pinned to
  // the line that carries a stack prefix, for the same reason.
  const stackLine = (page: Page) =>
    page
      .locator('[data-testid="log-line"][data-level="ERROR"]')
      .filter({ has: page.locator('[data-testid="stack-prefix"]') })
      .first();

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

    // Structured deploy line: a [web] stack-prefix next to the status glyph that
    // replaces the level badge on a narrated line (the console renders the same
    // glyph). Unlike the child line it has a stack; unlike an unnarrated line it
    // has no level badge.
    const stack = stackLine(page);
    await expect(stack).toBeVisible();
    await expect(stack.locator('[data-testid="stack-prefix"]')).toHaveText('[web]');
    await expect(stack.locator('[data-testid="log-glyph"]')).toBeVisible();
    await expect(stack.locator('[data-testid="level-badge"]')).toHaveCount(0);
    await expect(stack.locator('[data-testid="cmd-prefix"]')).toHaveCount(0);
  });
});

// UB6 — Diff pill in the log view. A `deploy complete` line carries the deploy
// event's `event_id`, rendered as a `diff-pill`. Clicking it fetches
// `/api/events/{id}/diffs` and inserts the same `diff-panel` directly below the
// log line (clicking again collapses it) — the log-view twin of UA8's deploy-row
// diff. The startup deploy is the stack's first (no prior commit → empty diffs); a
// second deploy that bumps the compose image is the first with a real diff, so its
// `deploy complete` line — the newest, hence the last one in chronological
// order — is the one whose pill expands a populated panel. We drive the real backend
// (webhook → deploy → SSE) and select only by `data-testid`.
test.describe('UB6: diff pill in logs', () => {
  // Both deploy-complete lines carry a diff-pill; the newest is the *last* one
  // in chronological order — the second deploy, the only one with a real diff.
  const newestDiffPill = (page: Page) =>
    page.locator('[data-testid="log-line"] [data-testid="diff-pill"]').last();
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

// UB7 — Log stream recovers from a fatal error. The /api/logs stream has no
// connection indicator, so a fatal error (non-2xx / bad content-type, which
// closes EventSource for good) must recover silently or the pane freezes with
// nothing on screen to explain it. We force the fatal path with a 503 route,
// then lift it and relaunch: the page's own backoff reconnect must re-open the
// stream so live lines flow again. Fails without the manual retry (the browser
// never comes back from CLOSED, so no further lines ever arrive).
test('UB7: log stream reconnects after a fatal error and resumes live lines', async ({
  page,
  skipper,
}) => {
  const warnLines = page.locator('[data-testid="log-line"][data-level="WARN"]');

  await page.goto(`${skipper.baseURL}/`);
  await page.locator('[data-testid="view-toggle"] button[data-view="logs"]').click();

  // The stream is live: a bad-signature webhook logs a WARN line that streams in.
  expect(await skipper.sendBadWebhook('refs/heads/main')).toBe(401);
  await expect(warnLines).toHaveCount(1);

  // Make every *new* /api/logs request fail fatally, then drop the live stream:
  // the browser auto-reconnects, hits the 503, and lands in CLOSED for good.
  await page.route('**/api/logs', (route) => route.fulfill({ status: 503 }));
  await skipper.stop();

  // Lift the fault, bring the backend back, and log another WARN. The pre-drop
  // line stays in the DOM, so recovery is proven only by the count *growing* —
  // a second WARN can arrive only if the stream reconnected, which the browser
  // will not do (it gave up at CLOSED); the page's own backoff retry must.
  await page.unroute('**/api/logs');
  await skipper.relaunch();
  expect(await skipper.sendBadWebhook('refs/heads/main')).toBe(401);
  await expect(async () => {
    expect(await warnLines.count()).toBeGreaterThan(1);
  }).toPass({ timeout: 20000 });
});

// UB8 — Logs view panel controls. The view is styled as one big clog-panel
// (same header chrome as the Stacks/Deploys container-log popup, just
// page-sized — see ut-container-logs.spec.ts), so search/wrap/fullscreen live
// inline in its own header — not behind the view-options popover, which
// carries no `logs` group at all now (see UD8). Search reveals the same filter
// bar via type-to-search or by clicking its tool; clicking the tool again
// closes it and clears the query, unlike the deploys/stacks filter's own clear
// button.
test.describe('UB8: Logs view panel controls (no popover)', () => {
  test.use({ startOptions: { stacks: ['web'] } });

  test('search/wrap/fullscreen tools sit in the panel header and work without a popover', async ({
    page,
    skipper,
  }) => {
    await page.goto(`${skipper.baseURL}/`);
    await openLogsView(page);

    const panel = page.locator('[data-testid="logs-panel"]');
    const searchBtn = page.locator('[data-testid="log-search"]');
    const wrapBtn = page.locator('[data-testid="log-wrap"]');
    const fsBtn = page.locator('[data-testid="log-fs"]');
    const filterWrap = page.locator('[data-testid="log-filter-wrap"]');
    const filterInput = page.locator('[data-testid="log-filter"]');

    // All three tools are visible immediately in the header — no popover to open.
    await expect(page.locator('[data-testid="view-options"]')).toBeHidden();
    await expect(searchBtn).toBeVisible();
    await expect(wrapBtn).toBeVisible();
    await expect(fsBtn).toBeVisible();

    // Type-to-search reveals the filter bar, seeded with the typed text, and
    // lights the search tool.
    await page.keyboard.type('deploy', { delay: 25 });
    await expect(filterWrap).toHaveClass(/revealed/);
    await expect(filterInput).toHaveValue('deploy');
    await expect(searchBtn).toHaveClass(/\bon\b/);

    // Clicking the search tool again closes it and clears the query.
    await searchBtn.click();
    await expect(filterWrap).not.toHaveClass(/revealed/);
    await expect(filterInput).toHaveValue('');
    await expect(searchBtn).not.toHaveClass(/\bon\b/);

    // Clicking it once more reopens an empty, focused filter.
    await searchBtn.click();
    await expect(filterWrap).toHaveClass(/revealed/);
    await expect(filterInput).toBeFocused();
    await page.keyboard.press('Escape'); // close it before the wrap/fullscreen checks below

    // Wrap and fullscreen light their own `.on` state directly on the tool —
    // no popover row to find them in.
    await wrapBtn.click();
    await expect(wrapBtn).toHaveClass(/\bon\b/);
    await expect(page.locator('#log-pane')).toHaveClass(/wrap/);

    await fsBtn.click();
    await expect(fsBtn).toHaveClass(/\bon\b/);
    await expect(panel).toHaveClass(/clog-fullscreen/);

    // Esc exits fullscreen (the header stays reachable to toggle it back off).
    await page.keyboard.press('Escape');
    await expect(panel).not.toHaveClass(/clog-fullscreen/);
    await expect(fsBtn).not.toHaveClass(/\bon\b/);
  });
});

// UB9 — Live/pause pill. Unlike the container-log panel's pause (which simply
// drops lines that arrive while paused), the Logs view's client-side buffer
// keeps growing regardless — pausing only freezes what's rendered. Going live
// again re-renders the window, so nothing seen elsewhere ever gets lost here.
test.describe('UB9: Logs view live/pause pill', () => {
  test.use({ startOptions: { stacks: ['web'] } });

  test('pausing freezes the pane; unpausing catches up everything buffered meanwhile', async ({
    page,
    skipper,
  }) => {
    const lines = page.locator('[data-testid="log-line"]');
    const live = page.locator('[data-testid="logs-live"]');
    const stat = page.locator('[data-testid="logs-stat"]');

    await page.goto(`${skipper.baseURL}/`);
    await openLogsView(page);
    await expect(lines.first()).toBeVisible();

    await expect(live).not.toHaveClass(/paused/);
    await expect(stat).toHaveText('live · streaming');

    const before = await lines.count();

    // Pausing flips the pill and footer immediately.
    await live.click();
    await expect(live).toHaveClass(/paused/);
    await expect(live).toContainText('paused');
    await expect(stat).toHaveText('paused');

    // Two lines stream in while paused — sequential, awaited round trips, so by
    // the time both have returned, any of the (former, buggy) immediate-render
    // behaviour would already be visible. Neither renders while paused.
    expect(await skipper.sendBadWebhook('refs/heads/main')).toBe(401);
    expect(await skipper.sendBadWebhook('refs/heads/main')).toBe(401);
    await expect(lines).toHaveCount(before);

    // Going live again catches both up in one render, and the footer/pill
    // revert.
    await live.click();
    await expect(live).not.toHaveClass(/paused/);
    await expect(stat).toHaveText('live · streaming');
    await expect(lines).toHaveCount(before + 2);
  });
});

// UB10 — Live/pause pill is keyboard-operable. The pill is a `role="button"`
// span (tabindex 0), so Enter and Space must toggle it exactly like a click —
// AT users otherwise get a control that announces as a button but does nothing.
test.describe('UB10: Logs view live/pause pill — keyboard', () => {
  test.use({ startOptions: { stacks: ['web'] } });

  test('Enter and Space toggle pause just like a click', async ({ page, skipper }) => {
    const live = page.locator('[data-testid="logs-live"]');
    const stat = page.locator('[data-testid="logs-stat"]');

    await page.goto(`${skipper.baseURL}/`);
    await openLogsView(page);
    await expect(live).not.toHaveClass(/paused/);

    // Focus the pill and press Enter: it pauses without any mouse click.
    await live.focus();
    await expect(live).toBeFocused();
    await page.keyboard.press('Enter');
    await expect(live).toHaveClass(/paused/);
    await expect(stat).toHaveText('paused');

    // Space resumes it — proving both activation keys are wired, not just Enter.
    await page.keyboard.press(' ');
    await expect(live).not.toHaveClass(/paused/);
    await expect(stat).toHaveText('live · streaming');
  });
});
