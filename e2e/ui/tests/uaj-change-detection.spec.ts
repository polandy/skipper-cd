import { test, expect } from '../fixtures/test';
import type { Page } from '@playwright/test';

// Maske AJ: the roster's change-detection panel. See docs/dev/e2e-tests.md §4.37.
//
// skipper redeploys a stack only when one of its hashed inputs changes, so the
// most common operator question is not "what happened" but "why did nothing
// happen". Every other surface leaves that to state.yaml on the host; the
// Triggers tab of a roster row's detail panel answers it in place — which inputs are watched,
// and the commit nothing has changed since. Behaviour-only (no snapshot): the
// panel is structural text, and the lead-line phrasing is exhaustively covered
// by the app-helpers unit layer (`watchedSummary`).

const stacksBtn = (page: Page) =>
  page.locator('[data-testid="view-toggle"] button[data-view="stacks"]');
const row = (page: Page, stack: string) =>
  page.locator(`[data-testid="roster-row"][data-stack="${stack}"]`);
const watched = (page: Page) => page.locator('[data-testid="watched-panel"]');
const lead = (page: Page) => page.locator('[data-testid="watched-lead"]');
const files = (page: Page) => page.locator('[data-testid="watched-file"]');

test.use({
  startOptions: {
    stacks: ['api', 'wip'],
    discovery: {
      repoConfig: 'stacks:\n  wip:\n    disabled: true\n',
      disabled: ['wip'],
    },
  },
});

// UAJ1 — a deployed stack names its watched inputs and the commit nothing has
// changed since, on the Triggers tab of the stack's detail panel. Without
// health data there is no Containers tab, so a clean stack opens on Triggers.
test('UAJ1: a deployed stack shows what is watched and since which commit', async ({
  page,
  skipper,
}) => {
  await page.goto(`${skipper.baseURL}/`);
  await stacksBtn(page).click();

  await row(page, 'api').click();
  const panel = page.locator('[data-testid="detail-panel"][data-stack="api"]');
  await expect(panel).toHaveAttribute('data-tab', 'triggers');
  await expect(panel.locator('[data-testid="watched-panel"]')).toHaveCount(1);
  await expect(watched(page)).toHaveAttribute('data-watched-for', 'api');
  // One tab at a time: the history is behind its own tab, not stacked above.
  await expect(page.locator('[data-testid="audit-panel"]')).toHaveCount(0);

  // The stack deployed cleanly at startup — the first deploy has no prior
  // commit to diff against, so the reference point is the deploy itself.
  await expect(lead(page)).toContainText(/^Unchanged since the last deploy\./);

  // Its compose file is listed, repo-relative — the path the operator edits and
  // commits, not the clone's absolute location on the host.
  await expect(files(page)).not.toHaveCount(0);
  const first = await files(page).first().textContent();
  expect(first).toContain('docker-compose.yml');
  expect(first!.startsWith('/')).toBe(false);

  // The stack's hashed config is recorded under a synthetic key, not a file:
  // it renders as its own prose entry, and never as a path in the file list.
  await expect(page.locator('[data-testid="watched-config"]')).toHaveCount(1);
  for (const text of await files(page).allTextContents()) {
    expect(text).not.toContain('skipper.yaml');
  }

  // Closing the row takes the panel with it — nothing left behind.
  await row(page, 'api').click();
  await expect(watched(page)).toHaveCount(0);
});

// UAJ1b — once a stack has deployed a pushed change, the lead names the commit
// its inputs have not changed since. This is the answer to "I pushed and this
// stack did nothing": the SHA it is already at.
test('UAJ1b: after a deployed change the lead names the commit', async ({ page, skipper }) => {
  await page.goto(`${skipper.baseURL}/`);

  // Only a deploy driven by a push carries a commit — the startup one records
  // none, since nothing was pushed to reach it. So let the startup deploy settle
  // *before* changing anything: pushed too early, the change is swept up by the
  // run already in flight and the lead legitimately reads "the last deploy"
  // forever. Row counts are the seam; nothing here waits on the clock.
  const deployed = page.locator('[data-testid="deploy-row"][data-stack="api"][data-status="success"]');
  await expect(deployed).toHaveCount(1);

  skipper.setStackImage('api', '1.26');
  expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);
  // The second success row is the pushed change's own deploy, and its arrival is
  // also the signal that the `stacks` snapshot has been republished.
  await expect(deployed).toHaveCount(2);

  await stacksBtn(page).click();
  await row(page, 'api').click();
  await expect(lead(page)).toContainText(/^Unchanged since [0-9a-f]{7}\./);
});

// UAJ2 — a parked stack is not watched at all, so the panel says so instead of
// listing whatever its last deploy happened to record.
test('UAJ2: a parked stack reports that nothing is watched', async ({ page, skipper }) => {
  await page.goto(`${skipper.baseURL}/`);
  await stacksBtn(page).click();

  await row(page, 'wip').click();
  await expect(watched(page)).toHaveCount(1);
  await expect(lead(page)).toContainText('Parked');
  await expect(files(page)).toHaveCount(0);
});

// UAJ3 — the panel obeys the roster's search filter like every other trailing
// panel: a row filtered out takes its whole expand card with it.
test('UAJ3: the panel hides with its row under an active filter', async ({ page, skipper }) => {
  await page.goto(`${skipper.baseURL}/`);
  await stacksBtn(page).click();

  await row(page, 'api').click();
  await expect(watched(page)).toBeVisible();

  await page.locator('[data-testid="stack-search-btn"]').click();
  await page.locator('[data-testid="roster-filter"]').fill('wip');
  await expect(watched(page)).toBeHidden();

  await page.locator('[data-testid="roster-filter"]').fill('');
  await expect(watched(page)).toBeVisible();
});
