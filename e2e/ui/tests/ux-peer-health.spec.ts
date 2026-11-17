import { test, expect } from '../fixtures/test';
import type { Page } from '@playwright/test';

// Maske X: Peer health parity (docs/dev/multi-host-spec.md, ADR-0048).
// See docs/dev/e2e-tests.md §4.25.
//
// The primary fans in each peer's health / healthwatch / app_links (not just its
// roster + deploys), so a peer's rows reach the same at-a-glance detail as a
// local stack: a live health pill inline on the roster row, an app-link in its
// detail head, and — on expand — the containers panel rendered
// from the peer's fanned-in health. The harness stub peer (host-b) now serves
// health/healthwatch/app_links in its snapshot; local health is enabled with
// healthPoll so the parity is visible on both sides. Behaviour-only. (The
// per-service log button that rides the peer's containers is proxied — its
// streaming behaviour is Maske UY.)

const iso = (minsAgo: number) => new Date(Date.now() - minsAgo * 60_000).toISOString();

// A reachable peer whose snapshot carries the full parity set: a roster, live
// health (gitea healthy, postgres unhealthy), a healthwatch timeline for
// postgres, an app link for gitea, plus matching audit records.
const hostB = {
  name: 'host-b',
  snapshot: {
    stacks: {
      roster: [
        { name: 'gitea', disabled: false, last_status: 'success', last_at: iso(3), last_commit: 'aaa1111' },
        { name: 'postgres', disabled: false, last_status: 'failed', last_at: iso(9), last_commit: 'bbb2222' },
      ],
      disabled: [],
    },
    health: {
      stacks: {
        gitea: { status: 'healthy', services: [{ name: 'gitea', state: 'running', status: 'healthy' }] },
        postgres: { status: 'unhealthy', services: [{ name: 'db', state: 'restarting', status: 'unhealthy' }] },
      },
    },
    healthwatch: {
      stacks: {
        postgres: {
          db: [
            { status: 'unhealthy', since: iso(4) },
            { status: 'healthy', since: iso(30) },
          ],
        },
      },
    },
    app_links: { stacks: { gitea: ['gitea.host-b.lan'] } },
  },
  audit: [
    { stack: 'gitea', status: 'success', timestamp: iso(3), duration_ms: 1400, changed_files: 1, commit_sha: 'aaa1111', id: 42 },
    { stack: 'postgres', status: 'failed', timestamp: iso(9), duration_ms: 800, changed_files: 2, commit_sha: 'bbb2222', id: 7 },
  ],
};

test.use({
  startOptions: {
    stacks: ['api', 'web'],
    hostName: 'host-a',
    healthPoll: 1, // local health poller, so local rows show pills too (parity)
    peers: [hostB],
  },
});

const deployRow = (page: Page, host: string, stack: string) =>
  page.locator(`[data-testid="deploy-row"][data-host="${host}"][data-stack="${stack}"]`);
const rosterRow = (page: Page, host: string, stack: string) =>
  page.locator(`[data-testid="roster-row"][data-host="${host}"][data-stack="${stack}"]`);
const stacksBtn = (page: Page) => page.locator('[data-testid="view-toggle"] button[data-view="stacks"]');

// UX2 — the merged Stacks roster shows the peer's live health pill inline on
// the row, so the overview answers "is it healthy" without opening anything; the
// peer's app-link rides the head of its detail panel.
test('UX2: peer roster rows show an inline health pill; the panel carries the app-link', async ({ page, skipper }) => {
  skipper.setStackHealth('api', [{ Service: 'app', State: 'running', Health: 'healthy' }]);
  skipper.setStackHealth('web', [{ Service: 'app', State: 'running', Health: 'healthy' }]);
  await page.goto(`${skipper.baseURL}/`);
  await stacksBtn(page).click();
  await expect(page.locator('[data-testid="stacks-view"]')).toBeVisible();

  const gitea = rosterRow(page, 'host-b', 'gitea');
  await expect(gitea.locator('[data-testid="health-pill"]')).toHaveAttribute('data-health', 'healthy');
  await expect(gitea.locator('[data-testid="app-link-btn"]')).toHaveCount(0);
  await gitea.click();
  const panel = page.locator('[data-testid="detail-panel"].peer-panel[data-host="host-b"][data-stack="gitea"]');
  await expect(panel.locator('[data-testid="app-link-btn"]')).toBeVisible();
  await expect(rosterRow(page, 'host-b', 'postgres').locator('[data-testid="health-pill"]')).toHaveAttribute('data-health', 'unhealthy');

  // Local roster rows carry the pill too — the roster showed no live health for
  // anyone before; now it does, uniformly.
  await expect(rosterRow(page, 'host-a', 'api').locator('[data-testid="health-pill"]')).toHaveAttribute('data-health', 'healthy');
});

// UX3 — a peer deploy's detail reaches the peer's containers: its Show in
// Stacks action opens that host's Stacks detail, whose containers tab lists
// the fanned-in services.
test('UX3: a peer deploy detail shows its containers in Stacks', async ({ page, skipper }) => {
  await page.goto(`${skipper.baseURL}/`);
  const gitea = deployRow(page, 'host-b', 'gitea');
  await expect(gitea).toBeVisible();

  await gitea.click();
  const deployDetail = page.locator('[data-testid="detail-panel"][data-kind="deploy"][data-host="host-b"]');
  await expect(deployDetail).toHaveCount(1);
  await deployDetail.locator('[data-testid="jump-btn"]').click();

  const detail = page.locator('[data-testid="stacks-view"] [data-testid="detail-panel"][data-host="host-b"]');
  await expect(detail).toHaveCount(1);
  const health = detail.locator('[data-testid="health-panel"]');
  await expect(health).toBeVisible();
  await expect(health.locator('[data-testid="health-service"]')).toHaveCount(1);
  await expect(health).toContainText('gitea');
  // A per-service log button rides the peer's containers too (its stream is
  // proxied — the button's behaviour is exercised in Maske UY).
  await expect(health.locator('[data-testid="clog-btn"]')).toHaveCount(1);
});

// UX4 — the peer's healthwatch timeline fans in too, so the peer stack's
// containers show its status history — reached through the peer deploy's Show
// in Stacks, host-scoped, never a blank local health panel.
test('UX4: peer containers show the healthwatch timeline', async ({ page, skipper }) => {
  await page.goto(`${skipper.baseURL}/`);
  const postgres = deployRow(page, 'host-b', 'postgres');
  await expect(postgres).toBeVisible();

  await postgres.click();
  const deployDetail = page.locator('[data-testid="detail-panel"][data-kind="deploy"][data-host="host-b"]');
  await deployDetail.locator('[data-testid="jump-btn"]').click();
  const detail = page.locator('[data-testid="stacks-view"] [data-testid="detail-panel"][data-host="host-b"]');
  await expect(detail).toHaveAttribute('data-stack', 'postgres');
  // A failed stack's detail opens on its history; the containers are one tab away.
  await detail.locator('[data-testid="detail-tab"][data-tab="containers"]').click();
  const health = detail.locator('[data-testid="health-panel"]');
  await expect(health).toBeVisible();

  // The fanned-in healthwatch renders the per-service status timeline.
  await expect(health.locator('[data-testid="health-history"]')).toBeVisible();
  await expect(health.locator('[data-testid="health-phase"]').first()).toBeVisible();
});
