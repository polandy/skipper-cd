import { test, expect } from '../fixtures/test';
import { FORGE_URL } from '../fixtures/harness';
import type { Skipper } from '../fixtures/harness';
import type { Page } from '@playwright/test';

/** A commit link's href: the configured forge, then the FULL 40-char SHA. */
function expectForgeCommitHref(href: string | null, forge = FORGE_URL): void {
  expect(href).not.toBeNull();
  expect(href!.startsWith(`${forge}/commit/`)).toBe(true);
  expect(href!.slice(`${forge}/commit/`.length)).toMatch(/^[0-9a-f]{40}$/);
}

// Maske AL: commit SHAs link to the forge. See dev-docs/e2e-tests.md §4.39.
//
// Every SHA the UI prints — the roster's Commit column, the deploy-history rows,
// the diff panel's commit header, a peer's detail — is a link to that commit on
// the forge (`repo_web_url`, or one derived from `repo_url`). Covers the roster
// link and its full-SHA target (UAL1), that following it never also toggles the
// row's panel (UAL2), the panel/diff-header links (UAL3), a peer linking to its
// OWN forge rather than the primary's (UAL4) and the degradation to plain text
// when no forge is known (UAL5).

test.use({ startOptions: { stacks: ['api', 'web'] } });

const stacksBtn = (page: Page) => page.locator('[data-testid="view-toggle"] button[data-view="stacks"]');
const row = (page: Page, stack: string) => page.locator(`[data-testid="roster-row"][data-stack="${stack}"]`);
const rosterSha = (page: Page, stack: string) => row(page, stack).locator('.roster-sha');

const apiDeploys = (page: Page) =>
  page.locator('[data-testid="deploy-row"][data-stack="api"][data-status="success"]');

// pushApi deploys a real change to `api` and resolves once THAT run has landed.
// The startup deploy records no commit, so the roster's Commit cell only fills
// on a pushed one — and its success row is indistinguishable from the startup
// one except by count. Waiting for the second row is the deterministic "the
// pushed run finished" signal; waiting for "a success row" would be satisfied by
// the startup row that is already there.
async function pushApi(page: Page, skipper: Skipper): Promise<void> {
  await expect(apiDeploys(page)).toHaveCount(1); // the startup deploy
  skipper.setStackImage('api', '1.26');
  expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);
  await expect(apiDeploys(page)).toHaveCount(2);
}

async function deployAndOpenStacks(page: Page, skipper: Skipper): Promise<void> {
  await page.goto(`${skipper.baseURL}/`);
  await pushApi(page, skipper);
  await stacksBtn(page).click();
  await expect(page.locator('[data-testid="stacks-view"]')).toBeVisible();
}

// UAL1 — the roster's Commit cell is a link to that commit on the forge. It
// prints the short SHA but must target the FULL one: a 7-char prefix is a
// display convention, and some forges will not resolve it.
test('UAL1: the roster Commit cell links to the commit on the forge', async ({ page, skipper }) => {
  await deployAndOpenStacks(page, skipper);

  const sha = rosterSha(page, 'api');
  await expect(sha).toHaveText(/^[0-9a-f]{7}$/);

  const full = await sha.getAttribute('title'); // the cell's tooltip is the full SHA
  expect(full).toMatch(/^[0-9a-f]{40}$/);
  const href = await sha.getAttribute('href');
  expectForgeCommitHref(href);
  expect(href).toBe(`${FORGE_URL}/commit/${full}`);
  expect(await sha.textContent()).toBe(full!.slice(0, 7));

  // A dashboard is a live stream; following a commit must not navigate away from
  // it, and a new tab must not get scripting access back to this one.
  await expect(sha).toHaveAttribute('target', '_blank');
  await expect(sha).toHaveAttribute('rel', /noopener/);
});

// UAL2 — the SHA sits inside a row whose body toggles the expand panel. Clicking
// the link must do one thing only: the panel must not open behind the new tab.
test('UAL2: following the commit link does not also expand the row', async ({ page, skipper }) => {
  await deployAndOpenStacks(page, skipper);

  // Cancel the navigation itself (the forge is not reachable from the test) in
  // the capture phase, which runs before the app's own delegated row handler —
  // so what is asserted below is exactly that handler's behaviour, with no
  // network and no new tab in the way.
  await page.evaluate(() => {
    document.addEventListener(
      'click',
      (e) => {
        if ((e.target as Element).closest('a[href]')) e.preventDefault();
      },
      true,
    );
  });

  await rosterSha(page, 'api').click();
  await expect(page.locator('[data-testid="audit-panel"]')).toHaveCount(0);

  // The row itself still expands — the guard is scoped to the link, not the row.
  await row(page, 'api').locator('.roster-name').click();
  await expect(page.locator('[data-testid="audit-panel"]')).toHaveCount(1);
});

// UAL3 — the same treatment reaches the SHAs inside panels: the deploy-history
// rows and the diff panel's commit header. One helper renders them all, so they
// either all link or all do not.
test('UAL3: deploy-history and diff-header SHAs link to the same forge', async ({ page, skipper }) => {
  await page.goto(`${skipper.baseURL}/`);
  await pushApi(page, skipper);

  // Diff panel: the commit header's SHA chip. Rows are newest first, so the
  // pushed deploy — the one with commit metadata — is the first.
  await apiDeploys(page).first().locator('[data-testid="files-pill"]').click();
  const headSha = page.locator('[data-testid="commit-sha"]');
  await expect(headSha).toBeVisible();
  expectForgeCommitHref(await headSha.getAttribute('href'));

  // Deploy history: every recorded outcome's SHA.
  await stacksBtn(page).click();
  await row(page, 'api').locator('.roster-name').click();
  const auditSha = page.locator('[data-testid="audit-row"] .ar-sha').first();
  await expect(auditSha).toBeVisible();
  expectForgeCommitHref(await auditSha.getAttribute('href'));
});

// UAL4 — a peer tracks its own deploy repo on its own forge. Linking its commits
// through the primary's would point at a repo that never had them.
test.describe('peer commits', () => {
  const PEER_FORGE = 'https://forge.host-b.example/ops/deploy';
  const PEER_SHA = 'b'.repeat(40);

  test.use({
    startOptions: {
      stacks: ['api'],
      hostName: 'host-a',
      peers: [
        {
          name: 'host-b',
          snapshot: {
            stacks: {
              disabled: [],
              repo_web_url: PEER_FORGE,
              roster: [{ name: 'cache', disabled: false, last_status: 'success', last_at: new Date().toISOString(), last_commit: PEER_SHA }],
            },
          },
        },
      ],
    },
  });

  test('UAL4: a peer row links its commit to the peer own forge', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);
    await stacksBtn(page).click();

    const peerSha = page.locator('[data-testid="roster-row"][data-host="host-b"][data-stack="cache"] .roster-sha');
    await expect(peerSha).toHaveAttribute('href', `${PEER_FORGE}/commit/${PEER_SHA}`);
  });
});

// UAL5 — an instance that knows no forge (a clone from a local path, no
// repo_web_url) must render exactly what it rendered before links existed: the
// SHA as inert text, never a dead link.
test.describe('no forge configured', () => {
  test.use({ startOptions: { stacks: ['api'], repoWebURL: null } });

  test('UAL5: without a forge the SHA stays plain text', async ({ page, skipper }) => {
    await deployAndOpenStacks(page, skipper);

    const sha = rosterSha(page, 'api');
    await expect(sha).toHaveText(/^[0-9a-f]{7}$/);
    expect(await sha.evaluate((el) => el.tagName)).toBe('SPAN');
    await expect(page.locator('.roster-sha[href]')).toHaveCount(0);
  });
});
