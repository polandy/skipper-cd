import { test, expect } from '../fixtures/test';
import type { Page } from '@playwright/test';

// Maske Y: Peer container logs (dev-docs/multi-host-spec.md, ADR-0048).
// See dev-docs/e2e-tests.md §4.26.
//
// The last local-only affordance closed: a peer's container logs. The browser
// can't reach a peer cross-origin, so the primary proxies the peer's
// container-logs SSE stream at GET /api/peers/{name}/container-logs/{stack}
// [/{service}] (the streaming sibling of the diff proxy). In the UI the peer's
// containers panel now carries the same per-service log button local stacks have;
// clicking it opens the live log panel, fed through the proxy. The harness stub
// peer (host-b) streams canned SSE frames for gitea/web so the proxy has
// something to forward. Behaviour-only.

const iso = (minsAgo: number) => new Date(Date.now() - minsAgo * 60_000).toISOString();

// A reachable peer with one healthy stack (so its containers panel renders a
// service line with a log button) and canned container-log frames for it.
const hostB = {
  name: 'host-b',
  snapshot: {
    stacks: {
      roster: [{ name: 'gitea', disabled: false, last_status: 'success', last_at: iso(3), last_commit: 'aaa1111' }],
      disabled: [],
    },
    health: {
      stacks: { gitea: { status: 'healthy', services: [{ name: 'web', state: 'running', status: 'healthy' }] } },
    },
  },
  audit: [
    { stack: 'gitea', status: 'success', timestamp: iso(3), duration_ms: 1400, changed_files: 1, commit_sha: 'aaa1111', id: 42 },
  ],
  logsByStack: {
    'gitea/web': ['2026-07-23T10:00:00Z web accepting connections', '2026-07-23T10:00:01Z web ready'],
  },
};

test.use({
  startOptions: {
    stacks: ['api'],
    hostName: 'host-a',
    healthPoll: 1,
    peers: [hostB],
  },
});

const deployRow = (page: Page, host: string, stack: string) =>
  page.locator(`[data-testid="deploy-row"][data-host="${host}"][data-stack="${stack}"]`);

// UY1 — a peer container's log button streams that peer's logs through the
// primary's proxy, into the same live panel a local container uses.
test('UY1: a peer container log button streams the peer log through the proxy', async ({ page, skipper }) => {
  await page.goto(`${skipper.baseURL}/`);

  // Expand the peer row → its containers panel (from the fanned-in health).
  await deployRow(page, 'host-b', 'gitea').click();
  const svc = page
    .locator('[data-testid="peer-detail"] [data-testid="health-panel"] [data-testid="health-service"]')
    .first();
  await expect(svc).toBeVisible();

  // The per-service log button opens the live log panel, fed by the proxy.
  await svc.locator('[data-testid="clog-btn"]').click();
  const panel = page.locator('[data-testid="clog-panel"]');
  await expect(panel).toBeVisible();
  const body = panel.locator('[data-testid="clog-body"]');
  await expect(body).toContainText('web accepting connections');
  await expect(body).toContainText('web ready');
});

// UY2 — the peer log button toggles closed on a second click, like a local one.
test('UY2: clicking the peer container log button again closes the panel', async ({ page, skipper }) => {
  await page.goto(`${skipper.baseURL}/`);

  await deployRow(page, 'host-b', 'gitea').click();
  const btn = page
    .locator('[data-testid="peer-detail"] [data-testid="health-panel"] [data-testid="health-service"]')
    .first()
    .locator('[data-testid="clog-btn"]');

  await btn.click();
  await expect(page.locator('[data-testid="clog-panel"]')).toBeVisible();
  await btn.click();
  await expect(page.locator('[data-testid="clog-panel"]')).toHaveCount(0);
});
