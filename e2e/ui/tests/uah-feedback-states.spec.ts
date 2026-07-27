import { test, expect } from '../fixtures/test';
import type { Page, Locator } from '@playwright/test';
import { openRowMenu } from '../fixtures/menu';

// Maske AH: feedback & error states (T4.16 / T4.17). See dev-docs/e2e-tests.md §4.35.
//
// Two gaps this mask covers, both deterministic (no timers):
//   T4.17 — a loading skeleton distinct from the genuine-empty state. The deploy
//           table starts as a skeleton and only reveals "No deployments yet" once
//           the SSE end-of-replay `synced` marker confirms an empty history.
//           Holding /api/events holds that marker, so the skeleton stays up
//           until the test releases the gate.
//   T4.16 — a fetch that fails no longer masquerades as empty. The audit, diff
//           and peer-diff fetches show an amber load-error + Retry; the Retry
//           re-runs just that fetch. A route that fails once then continues
//           exercises both the failure and the recovery.

const loadingState = (page: Page) => page.locator('[data-testid="loading-state"]');
const emptyState = (page: Page) => page.locator('[data-testid="empty-state"]');
const deployTable = (page: Page) => page.locator('#deploy-table');
const loadError = (scope: Page | Locator) => scope.locator('[data-testid="load-error"]');

// A route that answers the first hit with `status`, then lets every later hit
// through to the real backend — so one handler drives both the failure and the
// recovery a Retry triggers, with no wall-clock waiting.
async function failOnce(page: Page, glob: string, status: number) {
  let hits = 0;
  await page.route(glob, async (route) => {
    hits += 1;
    if (hits === 1) return route.fulfill({ status, body: 'boom' });
    return route.continue();
  });
}

// ─── T4.17: loading skeleton vs. genuine-empty ───

// UAH1 — a stack-free instance: the skeleton holds until the (empty) history
// replays, then yields to the genuine-empty state, never the reverse.
test.describe('UAH1: loading skeleton yields to the empty state', () => {
  test.use({ startOptions: { stacks: [] } });

  test('skeleton shows while connecting, then "No deployments yet"', async ({ page, skipper }) => {
    // Hold the stream: the `synced` marker rides it, so nothing settles until
    // we release.
    let release!: () => void;
    const gate = new Promise<void>((r) => (release = r));
    await page.route('**/api/events', async (route) => {
      await gate;
      await route.continue();
    });

    await page.goto(`${skipper.baseURL}/`);

    // While held: skeleton up, both the table and the empty state suppressed.
    await expect(loadingState(page)).toBeVisible();
    await expect(emptyState(page)).toBeHidden();
    await expect(deployTable(page)).toBeHidden();

    release();

    // Once the empty history replays and `synced` lands, the skeleton retires
    // and the genuine-empty state (with its confirmed-fact copy) appears.
    await expect(loadingState(page)).toBeHidden();
    await expect(emptyState(page)).toBeVisible();
    await expect(emptyState(page)).toContainText('No deployments yet');
    await expect(deployTable(page)).toBeHidden();
  });
});

// UAH2 — an instance with deploys: the skeleton yields to the table (never a
// flash of the empty state) once the replayed history paints its rows.
test.describe('UAH2: loading skeleton yields to the deploy table', () => {
  test('skeleton shows while connecting, then the rows appear', async ({ page, skipper }) => {
    let release!: () => void;
    const gate = new Promise<void>((r) => (release = r));
    await page.route('**/api/events', async (route) => {
      await gate;
      await route.continue();
    });

    await page.goto(`${skipper.baseURL}/`);
    await expect(loadingState(page)).toBeVisible();
    await expect(deployTable(page)).toBeHidden();

    release();

    await expect(deployTable(page)).toBeVisible();
    await expect(page.locator('[data-testid="deploy-row"][data-stack="web"]')).toHaveCount(1);
    await expect(loadingState(page)).toBeHidden();
    await expect(emptyState(page)).toBeHidden(); // never flashed
  });
});

// ─── T4.16: fetch errors, retryable in place ───

// UAH3 — the deploy-history fetch fails: an amber load-error with a Retry that,
// once the fetch recovers, fills the panel with the real records.
test.describe('UAH3: audit history load-error + retry', () => {
  test('a failed history fetch shows a retryable error, then loads', async ({ page, skipper }) => {
    await failOnce(page, '**/api/audit**', 500);
    await page.goto(`${skipper.baseURL}/`);

    const newest = page.locator('[data-testid="deploy-row"][data-stack="web"]').first();
    await openRowMenu(newest);
    await newest.locator('[data-testid="history-btn"]').click();

    const panel = page.locator('[data-testid="audit-panel"]');
    const err = loadError(panel);
    await expect(err).toBeVisible();
    await expect(err).toContainText("Couldn't load deploy history");
    // A genuinely-empty history reads differently — this is not that.
    await expect(panel).not.toContainText('No recorded deploys');

    await err.locator('[data-testid="load-retry"]').click();
    await expect(loadError(panel)).toHaveCount(0);
    await expect(panel.locator('[data-testid="audit-row"]')).toHaveCount(1); // the startup deploy
  });
});

// UAH4 — the diff fetch fails: a load-error instead of a silent drop to the file
// list; Retry recovers the real diff panel.
test.describe('UAH4: diff load-error + retry', () => {
  test('a failed diff fetch shows a retryable error, then the diff', async ({ page, skipper }) => {
    await failOnce(page, '**/api/events/*/diffs', 500);
    await page.goto(`${skipper.baseURL}/`);

    // A webhook image bump commits against the startup commit → a diffable row.
    skipper.setStackImage('web', '1.26');
    expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);
    const diffRow = page.locator(
      '[data-testid="deploy-row"][data-stack="web"][data-status="success"][data-has-diffs="1"]',
    );
    await expect(diffRow).toHaveCount(1);

    await diffRow.locator('[data-testid="files-pill"]').click();
    const err = loadError(page);
    await expect(err).toBeVisible();
    await expect(err).toContainText("Couldn't load the diff");
    await expect(page.locator('[data-testid="diff-panel"]')).toHaveCount(0);

    await err.locator('[data-testid="load-retry"]').click();
    await expect(loadError(page)).toHaveCount(0);
    await expect(page.locator('[data-testid="diff-panel"]')).toBeVisible();
  });
});

// UAH5 — the peer-diff proxy fails (peer unreachable): the detail keeps its facts
// and shows a load-error in the diff slot instead of the slot vanishing; Retry
// loads the peer's diff.
const hostB = {
  name: 'host-b',
  snapshot: {
    stacks: {
      roster: [{ name: 'gitea', disabled: false, last_status: 'success', last_at: new Date().toISOString(), last_commit: 'aaa1111' }],
      disabled: [],
    },
    health: { stacks: {} },
    app_links: { stacks: {} },
  },
  audit: [{ stack: 'gitea', status: 'success', timestamp: new Date().toISOString(), duration_ms: 1400, changed_files: 1, commit_sha: 'aaa1111', id: 42 }],
  diffs: {
    '42': {
      diffs: { 'docker-compose.yml': 'diff --git a/docker-compose.yml b/docker-compose.yml\n-  image: gitea:1.21\n+  image: gitea:1.22\n' },
      commits: [{ sha: 'aaa1111', subject: 'bump gitea', author: 'a', date: new Date().toISOString() }],
    },
  },
};

test.describe('UAH5: peer-diff load-error + retry', () => {
  test.use({ startOptions: { stacks: ['web'], hostName: 'host-a', peers: [hostB] } });

  test('an unreachable peer shows a retryable error, then the peer diff', async ({ page, skipper }) => {
    await failOnce(page, '**/api/peers/*/events/*/diffs', 502);
    await page.goto(`${skipper.baseURL}/`);

    const peerRow = page.locator('[data-testid="deploy-row"][data-host="host-b"][data-stack="gitea"]');
    await expect(peerRow).toBeVisible();
    await peerRow.click();

    const detail = page.locator('[data-testid="peer-detail"]');
    await expect(detail).toContainText('aaa1111'); // the read-only facts remain
    const err = loadError(detail);
    await expect(err).toBeVisible();
    await expect(err).toContainText('reach host-b');

    await err.locator('[data-testid="load-retry"]').click();
    await expect(loadError(detail)).toHaveCount(0);
    await expect(detail.locator('[data-testid="peer-diff"] [data-testid="diff-panel"]')).toBeVisible();
    await expect(detail.locator('[data-testid="peer-diff"]')).toContainText('gitea:1.22');
  });
});
