import { test, expect } from '../fixtures/test';
import type { Page } from '@playwright/test';

// Maske V: Multi-host federated UI (dev-docs/multi-host-spec.md, ADR-0048).
// See dev-docs/e2e-tests.md §4.23.
//
// The primary (host-a) fans in each peer's read data and renders one merged UI.
// The harness stands up a reachable stub peer (host-b, serving its /api/v1
// snapshot + audit) and an unreachable one (host-c, a dead port). Covers: the
// merged deploy feed with per-row host dots (UV1), the dot as a click-to-filter
// toggle (UV2), the Hosts drawer multi-select filter (UV3), an offline peer's
// stale banner + reachability dot (UV4), and the merged roster with read-only
// peer rows (UV5). Behaviour-only (no snapshot); the host-colour assignment is
// unit-tested (app-helpers.test.js).

const iso = (minsAgo: number) => new Date(Date.now() - minsAgo * 60_000).toISOString();

// A reachable peer with two deploys + a matching roster.
const hostB = {
  name: 'host-b',
  snapshot: {
    stacks: {
      roster: [
        { name: 'gitea', disabled: false, last_status: 'success', last_at: iso(3), last_commit: 'aaa1111' },
        { name: 'forgejo', disabled: false, last_status: 'failed', last_at: iso(9), last_commit: 'bbb2222' },
      ],
      disabled: [],
    },
    health: { stacks: {} },
    app_links: { stacks: {} },
  },
  audit: [
    { stack: 'gitea', status: 'success', timestamp: iso(3), duration_ms: 1400, changed_files: 1, commit_sha: 'aaa1111', id: 42 },
    { stack: 'forgejo', status: 'failed', timestamp: iso(9), duration_ms: 800, changed_files: 2, commit_sha: 'bbb2222', id: 7 },
  ],
  // Diff the primary's proxy fetches for gitea's deploy (event id 42).
  diffs: {
    '42': {
      diffs: { 'docker-compose.yml': 'diff --git a/docker-compose.yml b/docker-compose.yml\n-  image: gitea:1.21\n+  image: gitea:1.22\n' },
      commits: [{ sha: 'aaa1111', subject: 'bump gitea', author: 'a', date: iso(3) }],
    },
  },
};

test.use({
  startOptions: {
    stacks: ['api', 'web'],
    hostName: 'host-a',
    peers: [hostB, { name: 'host-c', reachable: false }],
  },
});

const hostsBtn = (page: Page) => page.locator('[data-testid="hosts-btn"]');
const drawer = (page: Page) => page.locator('[data-testid="hosts-drawer"]');
const table = (page: Page) => page.locator('[data-testid="deploys-table"]');
const rowsFor = (page: Page, host: string) => page.locator(`[data-testid="deploy-row"][data-host="${host}"]`);
const stacksBtn = (page: Page) => page.locator('[data-testid="view-toggle"] button[data-view="stacks"]');
const rosterFor = (page: Page, host: string) => page.locator(`[data-testid="roster-row"][data-host="${host}"]`);

// UV1 — the Hosts control appears and the merged feed interleaves peer deploys,
// each row tagged with its host's colour dot.
test('UV1: merged deploy feed shows peer rows with host dots', async ({ page, skipper }) => {
  await page.goto(`${skipper.baseURL}/`);

  // The Hosts control is enabled and reports the host count (self + 2 peers).
  await expect(hostsBtn(page)).toBeVisible();
  await expect(hostsBtn(page)).toHaveClass(/enabled/);
  await expect(page.locator('[data-testid="hosts-btn"] #hosts-count')).toHaveText('3/3');

  // Local rows (host-a) plus the reachable peer's rows (host-b) are merged in.
  await expect(rowsFor(page, 'host-a')).toHaveCount(2); // api, web startup deploys
  await expect(rowsFor(page, 'host-b')).toHaveCount(2); // gitea, forgejo
  await expect(page.locator('[data-testid="deploy-row"][data-host="host-b"][data-stack="gitea"]')).toBeVisible();

  // Peer rows are read-only mirrors: a colour dot but no drill-down affordances.
  const gitea = page.locator('[data-testid="deploy-row"][data-host="host-b"][data-stack="gitea"]');
  await expect(gitea.locator('.host-mono')).toBeVisible();
  await expect(gitea.locator('[data-testid="history-btn"]')).toHaveCount(0);
  await expect(gitea.locator('[data-testid="jump-btn"]')).toHaveCount(0);

  // Local rows carry the dot too (dots are shown whenever peers are configured).
  await expect(rowsFor(page, 'host-a').first().locator('.host-mono')).toBeVisible();

  // Two hosts must render as visibly different colours (the merged view's whole
  // point): distinct palette slots and distinct computed dot colours.
  const dotA = rowsFor(page, 'host-a').first().locator('.host-mono');
  const dotB = rowsFor(page, 'host-b').first().locator('.host-mono');
  const slotA = await dotA.getAttribute('data-host-color');
  const slotB = await dotB.getAttribute('data-host-color');
  expect(slotA).not.toBeNull();
  expect(slotB).not.toBeNull();
  expect(slotA).not.toEqual(slotB);
  const colorA = await dotA.evaluate((el) => getComputedStyle(el).backgroundColor);
  const colorB = await dotB.evaluate((el) => getComputedStyle(el).backgroundColor);
  expect(colorA).not.toEqual(colorB);
});

// UV2 — clicking a host dot isolates the view to that host; clicking a dot again
// clears the filter. While filtered, the table wears the active-filter state.
test('UV2: clicking a host dot filters to that host and toggles back', async ({ page, skipper }) => {
  await page.goto(`${skipper.baseURL}/`);
  await expect(rowsFor(page, 'host-b')).toHaveCount(2);

  // Click host-b's dot → only host-b rows remain; local rows hidden.
  await rowsFor(page, 'host-b').first().locator('.host-mono').click();
  await expect(table(page)).toHaveClass(/host-filter-active/);
  await expect(rowsFor(page, 'host-a').first()).toBeHidden();
  await expect(rowsFor(page, 'host-b').first()).toBeVisible();
  await expect(page.locator('[data-testid="hosts-btn"] #hosts-count')).toHaveText('1/3');

  // Click a (visible, host-b) dot again → filter cleared, everything back.
  await rowsFor(page, 'host-b').first().locator('.host-mono').click();
  await expect(table(page)).not.toHaveClass(/host-filter-active/);
  await expect(rowsFor(page, 'host-a').first()).toBeVisible();
  await expect(page.locator('[data-testid="hosts-btn"] #hosts-count')).toHaveText('3/3');
});

// UV3 — the Hosts drawer multi-select filters the merged feed; "Select all"
// restores every host.
test('UV3: the Hosts drawer multi-select filters the feed', async ({ page, skipper }) => {
  await page.goto(`${skipper.baseURL}/`);
  await expect(rowsFor(page, 'host-b')).toHaveCount(2);

  await hostsBtn(page).click();
  await expect(drawer(page)).toHaveClass(/\bopen\b/);
  // One row per host: self + 2 peers.
  await expect(page.locator('[data-testid="host-row"]')).toHaveCount(3);

  // Deselect host-b → its rows drop out; local rows stay.
  await page.locator('[data-testid="host-row"][data-host="host-b"]').click();
  await expect(rowsFor(page, 'host-b').first()).toBeHidden();
  await expect(rowsFor(page, 'host-a').first()).toBeVisible();

  // "Select all" clears the filter.
  await page.locator('[data-testid="hosts-all-btn"]').click();
  await expect(rowsFor(page, 'host-b').first()).toBeVisible();
});

// UV4 — an unreachable peer is flagged, not blanked: the Hosts control shows the
// offline state, a stale banner names the peer, and its drawer row reads down.
test('UV4: an unreachable peer shows the offline banner and a down dot', async ({ page, skipper }) => {
  await page.goto(`${skipper.baseURL}/`);
  await expect(hostsBtn(page)).toHaveClass(/enabled/);

  // The control flags an offline peer; the stale banner names host-c.
  await expect(hostsBtn(page)).toHaveClass(/has-offline/);
  const banner = page.locator('[data-testid="host-stale-banner"]');
  await expect(banner).toBeVisible();
  await expect(banner).toContainText('host-c');

  // In the drawer, host-c's reachability dot reads down; host-b reads up.
  await hostsBtn(page).click();
  await expect(page.locator('[data-testid="host-row"][data-host="host-c"] .host-dot')).toHaveClass(/down/);
  await expect(page.locator('[data-testid="host-row"][data-host="host-b"] .host-dot')).toHaveClass(/up/);
  // A peer offers an "open its own UI" link; self does not.
  await expect(page.locator('[data-testid="host-row"][data-host="host-b"] [data-testid="host-link"]')).toHaveCount(1);
  await expect(page.locator('[data-testid="host-row"][data-host="host-a"] [data-testid="host-link"]')).toHaveCount(0);
});

// UV5 — the Stacks view merges peer stacks too, host-tagged and read-only.
test('UV5: the merged roster shows peer stacks, read-only', async ({ page, skipper }) => {
  await page.goto(`${skipper.baseURL}/`);
  await stacksBtn(page).click();
  await expect(page.locator('[data-testid="stacks-view"]')).toBeVisible();

  // Local stacks (host-a) and the reachable peer's stacks (host-b) both listed.
  await expect(rosterFor(page, 'host-a')).toHaveCount(2); // api, web
  await expect(rosterFor(page, 'host-b')).toHaveCount(2); // gitea, forgejo

  const gitea = page.locator('[data-testid="roster-row"][data-host="host-b"][data-stack="gitea"]');
  await expect(gitea).toBeVisible();
  await expect(gitea.locator('.host-mono')).toBeVisible();
  await expect(gitea.locator('[data-testid="status-badge"]')).toHaveText('success');
  // Read-only mirror: no jump / logs affordances on a peer's stack.
  await expect(gitea.locator('[data-testid="jump-btn"]')).toHaveCount(0);
  await expect(gitea.locator('[data-testid="clog-btn"]')).toHaveCount(0);

  // The host filter reaches the roster: isolate to host-b via its dot.
  await gitea.locator('.host-mono').click();
  await expect(rosterFor(page, 'host-a').first()).toBeHidden();
  await expect(gitea).toBeVisible();
});

// UV6 — a peer row is a read-only mirror, but a click never dead-ends: it opens
// a compact detail (commit + file count) with a link to the peer's own UI. This
// is the case behind the "clicking the peer _nixos row does nothing" report.
test('UV6: clicking a peer row opens its read-only detail with a peer-UI link', async ({ page, skipper }) => {
  await page.goto(`${skipper.baseURL}/`);
  const peerRow = page.locator('[data-testid="deploy-row"][data-host="host-b"][data-stack="gitea"]');
  await expect(peerRow).toBeVisible();
  await expect(page.locator('[data-testid="peer-detail"]')).toHaveCount(0);

  await peerRow.click();
  const detail = page.locator('[data-testid="peer-detail"]');
  await expect(detail).toHaveCount(1);
  await expect(detail).toContainText('aaa1111'); // the peer record's commit
  await expect(detail).toContainText('1 file changed');
  await expect(detail.locator('[data-testid="peer-detail-link"]')).toHaveAttribute('href', /^http:\/\/127\.0\.0\.1/);

  // The peer's diff is fetched through the primary's proxy and rendered inline.
  await expect(detail.locator('[data-testid="peer-diff"] [data-testid="diff-panel"]')).toBeVisible();
  await expect(detail.locator('[data-testid="peer-diff"]')).toContainText('gitea:1.22');

  // Clicking again closes it.
  await peerRow.click();
  await expect(page.locator('[data-testid="peer-detail"]')).toHaveCount(0);
});
