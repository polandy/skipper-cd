import { test, expect } from '../fixtures/test';
import type { Page } from '@playwright/test';
import type { OrphanContainer } from '../fixtures/harness';

// Maske Q: orphan detection (ADR-0036). See dev-docs/e2e-tests.md §4.17.
//
// Boots in discovery mode (web + api are the stack set) with the health poll on,
// since orphan detection rides that cadence and is UI-gated. The stub's
// `docker ps -a` listing is scripted with setOrphans: the two active stacks
// (managed, excluded from the section), a removed stack still running under
// stacks_base_dir (orphaned, two containers), and a hand-started project outside
// it (unmanaged). Behaviour-only (no snapshot).

test.use({ startOptions: { stacks: ['web', 'api'], discovery: {}, healthPoll: 1 } });

// orphanListing is the set of compose containers docker "sees": the managed pair
// (web, api) plus an orphaned and an unmanaged project, keyed off the instance's
// stacks_base_dir so the working_dir classification is deterministic.
function orphanListing(base: string): OrphanContainer[] {
  return [
    { project: 'web', workingDir: `${base}/web`, name: 'web-1', service: 'app', image: 'nginx:1.25', state: 'running', status: 'Up 4 minutes' },
    { project: 'api', workingDir: `${base}/api`, name: 'api-1', service: 'app', image: 'nginx:1.25', state: 'running', status: 'Up 4 minutes' },
    // Removed stack, still running under stacks_base_dir -> orphaned, prunable.
    { project: 'legacy-cache', workingDir: `${base}/legacy-cache`, name: 'legacy-cache-redis-1', service: 'redis', image: 'redis:7', state: 'running', status: 'Up 5 days', ports: '0.0.0.0:6379->6379/tcp' },
    { project: 'legacy-cache', workingDir: `${base}/legacy-cache`, name: 'legacy-cache-worker-1', service: 'worker', image: 'redis:7', state: 'exited', status: 'Exited (0) 2 days ago' },
    // Hand-started project outside stacks_base_dir -> unmanaged, never pruned.
    { project: 'portainer', workingDir: '/opt/portainer', name: 'portainer', service: 'portainer', image: 'portainer/portainer-ce:2.19', state: 'running', status: 'Up 3 weeks', ports: '0.0.0.0:9000->9000/tcp' },
  ];
}

const section = (p: Page) => p.locator('[data-testid="orphans"]');
const head = (p: Page) => p.locator('[data-testid="orphans-head"]');
const count = (p: Page) => p.locator('[data-testid="orphans-count"]');
const item = (p: Page, project: string) =>
  p.locator(`[data-testid="orphan-item"][data-project="${project}"]`);
const filterCount = (p: Page) => p.locator('#deploy-filter-count');

// UQ1 — detection surfaces the orphaned + unmanaged projects (managed excluded),
// and each row expands to its containers plus the data-safety facts.
test('UQ1: lists orphaned + unmanaged projects, expandable to containers and data-safety facts', async ({
  page,
  skipper,
}) => {
  skipper.setOrphans(orphanListing(skipper.stacksBaseDir));
  skipper.setVolumes([
    { project: 'legacy-cache', volume: 'legacy-cache_redis-data' },
    { project: 'legacy-cache', volume: 'legacy-cache_backups' },
    { project: 'web', volume: 'web_data' }, // managed: never surfaced
  ]);
  await page.goto(`${skipper.baseURL}/`);

  // The section appears with a count of 2: web and api are matched by working_dir
  // and excluded; only the orphaned + unmanaged projects remain.
  await expect(head(page)).toBeVisible();
  await expect(count(page)).toHaveText('2');
  await expect(item(page, 'web')).toHaveCount(0);
  await expect(item(page, 'api')).toHaveCount(0);

  // Collapsed by default; open the section to reveal the two items, correctly
  // classified.
  await head(page).click();
  await expect(section(page)).toHaveClass(/\bopen\b/);
  await expect(item(page, 'legacy-cache')).toHaveAttribute('data-class', 'orphaned');
  await expect(item(page, 'portainer')).toHaveAttribute('data-class', 'unmanaged');

  // Expand the orphaned project: its two containers (one running, one exited) and
  // the data-safety facts (compose path, named volumes kept on prune) appear.
  const legacy = item(page, 'legacy-cache');
  await legacy.locator('.orphan-row').click();
  await expect(legacy).toHaveClass(/\bopen\b/);
  await expect(legacy.locator('[data-testid="orphan-cont"]')).toHaveCount(2);
  await expect(legacy).toContainText(`${skipper.stacksBaseDir}/legacy-cache/docker-compose.yml`);
  await expect(legacy).toContainText('legacy-cache_redis-data');
  await expect(legacy).toContainText('kept on prune');
});

// UQ2 — the deploy search also scans orphans: a match auto-expands its row and
// hides the non-matching orphans; the badge and the hits/total count follow.
test('UQ2: the deploy search scans orphans — matches auto-expand, non-matches hide, counts follow', async ({
  page,
  skipper,
}) => {
  skipper.setOrphans(orphanListing(skipper.stacksBaseDir));
  await page.goto(`${skipper.baseURL}/`);
  await expect(count(page)).toHaveText('2'); // both orphans, no search yet

  // A term only an orphan *container* carries (the redis image): the section
  // auto-opens, legacy-cache matches and auto-expands with the hit, and the
  // non-matching orphan is hidden. Badge -> 1.
  await page.keyboard.type('redis');
  await expect(section(page)).toHaveClass(/\bopen\b/);
  await expect(item(page, 'legacy-cache')).toBeVisible();
  await expect(item(page, 'legacy-cache')).toHaveClass(/\bopen\b/);
  await expect(item(page, 'portainer')).toBeHidden();
  await expect(count(page)).toHaveText('1');
  // hits/total counts orphans among the searchable elements: 0 of 2 deploy rows
  // + 1 of 2 orphans = 1/4.
  await expect(filterCount(page)).toHaveText('1/4');

  // Switch the query to the unmanaged project's name: now it is the only match.
  await page.keyboard.press('Escape'); // first Esc clears the query, keeps the bar
  await page.keyboard.type('portainer');
  await expect(item(page, 'portainer')).toBeVisible();
  await expect(item(page, 'legacy-cache')).toBeHidden();
  await expect(count(page)).toHaveText('1');
  await expect(filterCount(page)).toHaveText('1/4');

  // A term nothing matches: every orphan hidden, badge 0.
  await page.keyboard.press('Escape');
  await page.keyboard.type('zzz');
  await expect(item(page, 'legacy-cache')).toBeHidden();
  await expect(item(page, 'portainer')).toBeHidden();
  await expect(count(page)).toHaveText('0');
});

// UQ3 — a manually expanded orphan is not re-collapsed when fresh orphan data
// lands (the section re-renders every poll; the expansion is tracked client-side).
test('UQ3: a manually expanded orphan stays open across an orphan-data refresh', async ({
  page,
  skipper,
}) => {
  skipper.setOrphans(orphanListing(skipper.stacksBaseDir));
  await page.goto(`${skipper.baseURL}/`);
  await head(page).click(); // open the section
  const legacy = item(page, 'legacy-cache');
  await legacy.locator('.orphan-row').click(); // expand the row
  await expect(legacy).toHaveClass(/\bopen\b/);

  // Push a fresh detection (the worker container's status changes); the next poll
  // re-renders the section from the new snapshot.
  const changed = orphanListing(skipper.stacksBaseDir);
  changed.find((c) => c.name === 'legacy-cache-worker-1')!.status = 'Exited (0) 3 days ago';
  skipper.setOrphans(changed);

  await expect(legacy).toContainText('Exited (0) 3 days ago'); // the re-render landed
  await expect(legacy).toHaveClass(/\bopen\b/); // and the expansion held
});
