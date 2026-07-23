import { test, expect } from '../fixtures/test';
import type { Page } from '@playwright/test';

// Maske X: Peer health parity (dev-docs/multi-host-spec.md, ADR-0048).
// See dev-docs/e2e-tests.md §4.25.
//
// The primary fans in each peer's health / healthwatch / app_links (not just its
// roster + deploys), so a peer's rows reach the same at-a-glance detail as a
// local stack: a live health pill inline on the overview (no expand needed), an
// app-link on the roster row, and — on expand — the containers panel rendered
// from the peer's fanned-in health, read-only (no per-service log button, since
// the container-logs proxy is a follow-up). The harness stub peer (host-b) now
// serves health/healthwatch/app_links in its snapshot; local health is enabled
// with healthPoll so the parity is visible on both sides. Behaviour-only.

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

// UX1 — the merged Deploys overview shows a live health pill on the peer's rows,
// inline, without expanding — the same pill local rows carry.
test('UX1: peer deploy rows show an inline health pill', async ({ page, skipper }) => {
  skipper.setStackHealth('api', [{ Service: 'app', State: 'running', Health: 'healthy' }]);
  skipper.setStackHealth('web', [{ Service: 'app', State: 'running', Health: 'healthy' }]);
  await page.goto(`${skipper.baseURL}/`);

  // Peer rows carry the pill with the fanned-in status — no click needed.
  await expect(deployRow(page, 'host-b', 'gitea').locator('[data-testid="health-pill"]')).toHaveAttribute('data-health', 'healthy');
  await expect(deployRow(page, 'host-b', 'postgres').locator('[data-testid="health-pill"]')).toHaveAttribute('data-health', 'unhealthy');

  // Local rows show the pill too (parity between the primary and its peers).
  await expect(deployRow(page, 'host-a', 'api').locator('[data-testid="health-pill"]')).toHaveAttribute('data-health', 'healthy');
});

// UX2 — the merged Stacks roster shows the peer's live health pill and app-link
// inline on the row, so the overview answers "is it healthy" without expanding.
test('UX2: peer roster rows show an inline health pill and app-link', async ({ page, skipper }) => {
  skipper.setStackHealth('api', [{ Service: 'app', State: 'running', Health: 'healthy' }]);
  skipper.setStackHealth('web', [{ Service: 'app', State: 'running', Health: 'healthy' }]);
  await page.goto(`${skipper.baseURL}/`);
  await stacksBtn(page).click();
  await expect(page.locator('[data-testid="stacks-view"]')).toBeVisible();

  const gitea = rosterRow(page, 'host-b', 'gitea');
  await expect(gitea.locator('[data-testid="health-pill"]')).toHaveAttribute('data-health', 'healthy');
  await expect(gitea.locator('[data-testid="app-link-btn"]')).toBeVisible();
  await expect(rosterRow(page, 'host-b', 'postgres').locator('[data-testid="health-pill"]')).toHaveAttribute('data-health', 'unhealthy');

  // Local roster rows carry the pill too — the roster showed no live health for
  // anyone before; now it does, uniformly.
  await expect(rosterRow(page, 'host-a', 'api').locator('[data-testid="health-pill"]')).toHaveAttribute('data-health', 'healthy');
});

// UX3 — expanding a peer deploy row renders the peer's containers (health) panel
// inline, read-only: it lists the fanned-in services but drops the per-service
// log button (the container-logs proxy is a follow-up).
test('UX3: expanding a peer row shows its containers, read-only', async ({ page, skipper }) => {
  await page.goto(`${skipper.baseURL}/`);
  const gitea = deployRow(page, 'host-b', 'gitea');
  await expect(gitea).toBeVisible();

  await gitea.click();
  const detail = page.locator('[data-testid="peer-detail"]');
  await expect(detail).toHaveCount(1);

  // The containers panel rides inside the peer-detail, from the peer's health.
  const health = detail.locator('[data-testid="health-panel"]');
  await expect(health).toBeVisible();
  await expect(health.locator('[data-testid="health-service"]')).toHaveCount(1);
  await expect(health).toContainText('gitea');
  // Read-only mirror: no per-service log button on a peer's containers.
  await expect(health.locator('[data-testid="clog-btn"]')).toHaveCount(0);
});

// UX4 — the peer's healthwatch timeline fans in too, so an expanded peer stack
// shows its status history; and the inline pill is a shortcut into that detail,
// not a click into the primary's own (empty) local health.
test('UX4: peer containers show the healthwatch timeline; the pill opens the detail', async ({ page, skipper }) => {
  await page.goto(`${skipper.baseURL}/`);
  const postgres = deployRow(page, 'host-b', 'postgres');

  // Clicking the peer row's health pill opens its containers detail (host-scoped),
  // never a blank local health panel.
  await postgres.locator('[data-testid="health-pill"]').click();
  const detail = page.locator('[data-testid="peer-detail"]');
  await expect(detail).toHaveCount(1);
  const health = detail.locator('[data-testid="health-panel"]');
  await expect(health).toBeVisible();

  // The fanned-in healthwatch renders the per-service status timeline.
  await expect(health.locator('[data-testid="health-history"]')).toBeVisible();
  await expect(health.locator('[data-testid="health-phase"]').first()).toBeVisible();
});
