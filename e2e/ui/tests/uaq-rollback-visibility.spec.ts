import { test, expect } from '../fixtures/test';
import type { Page } from '@playwright/test';

// Maske AQ: after-the-fact rollback visibility. See dev-docs/e2e-tests.md §4.44.
//
// The 2026-08-05 incident: a rollback was recorded everywhere and visible
// nowhere five minutes later, because the successful retry became every
// surface's newest word on the stack. This mask drives that exact sequence —
// startup success, a deploy that rolls back (STUB_DOCKER_FAIL_NTH_UP=2:
// up#1 startup ok, up#2 fails, up#3 rollback ok), then a successful retry
// (up#4) — and asserts each surface keeps the rollback readable:
// the retry note pairing (UAQ1), the Deploys status filter (UAQ2), the header
// incident badge landing on the pre-filtered view (UAQ3), the roster's outcome
// strip + last-incident line (UAQ4), and the Logs severity threshold that
// keeps the WARN-level rollback outcome under the errors chip (UAQ5).
// Behaviour-only (no snapshot).

const deployRow = (page: Page, status: string) =>
  page.locator(`[data-testid="deploy-row"][data-stack="web"][data-status="${status}"]`);
const statusChip = (page: Page, status: string) =>
  page.locator(`[data-testid="deploy-status-filter"] .status-chip[data-status="${status}"]`);

// Drives the incident sequence and waits for the retry's success row: after it,
// the audit history for `web` reads success → rolled_back → success.
async function rollBackThenRetry(page: Page, skipper: import('../fixtures/harness').Skipper) {
  await page.goto(`${skipper.baseURL}/`);
  await expect(deployRow(page, 'success')).toHaveCount(1); // startup settled
  skipper.setStackImage('web', '1.26');
  expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);
  await expect(deployRow(page, 'rolled_back')).toHaveCount(1);
  skipper.setStackImage('web', '1.27');
  expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);
  await expect(deployRow(page, 'success')).toHaveCount(2);
}

test.describe('Maske AQ: rollback visibility after the fact', () => {
  test.use({ startOptions: { stacks: ['web'], stubEnv: { STUB_DOCKER_FAIL_NTH_UP: '2' } } });

  // UAQ1 — the retry's success row names the rollback it supersedes: a retry
  // note on the newest row only, carrying the rollback's event id; activating
  // it lands on (flashes) the rollback's own row.
  test('UAQ1: the retry note pairs the success with its rollback', async ({ page, skipper }) => {
    await rollBackThenRetry(page, skipper);

    const notes = page.locator('[data-testid="retry-note"]');
    await expect(notes).toHaveCount(1);
    // On the retry (the newest success), not the startup success.
    const noteRow = page.locator('[data-testid="deploy-row"]:has([data-testid="retry-note"])');
    await expect(noteRow).toHaveAttribute('data-status', 'success');
    const rollbackId = await deployRow(page, 'rolled_back').getAttribute('data-event-id');
    await expect(notes).toHaveAttribute('data-rollback-id', rollbackId!);

    await notes.click();
    await expect(deployRow(page, 'rolled_back')).toHaveClass(/jump-target/);
  });

  // UAQ2 — the status filter answers "show me the deploys that went wrong":
  // per-status chips with counts narrow the log, and Esc clears the narrowing
  // together with the query (never persisted).
  test('UAQ2: status chips narrow the deploy log and Esc clears them', async ({
    page,
    skipper,
  }) => {
    await rollBackThenRetry(page, skipper);

    await page.locator('[data-testid="stack-search-btn"]').click();
    await expect(page.locator('[data-testid="deploy-status-filter"]')).toBeVisible();
    await expect(statusChip(page, 'rolled_back').locator('.sc-count')).toHaveText('1');
    await expect(statusChip(page, 'success').locator('.sc-count')).toHaveText('2');

    // A chip click straight from the focused input must not fold the bar out
    // from under the click (the blur-before-click trap).
    await statusChip(page, 'rolled_back').click();
    await expect(statusChip(page, 'rolled_back')).toHaveAttribute('aria-pressed', 'true');
    await expect(deployRow(page, 'rolled_back')).toBeVisible();
    await expect(deployRow(page, 'success').first()).toBeHidden();
    await expect(page.locator('#deploy-filter-count')).toHaveText('1/3');

    // Esc clears the chips with the query; every row returns.
    await page.keyboard.press('Escape');
    await expect(deployRow(page, 'success').first()).toBeVisible();
    await expect(deployRow(page, 'rolled_back')).toBeVisible();
  });

  // UAQ3 — the header incident badge counts the last 24h's bad outcomes in
  // every view and lands on the Deploys view with the bad-outcome chips
  // pre-selected — the click shows exactly the rows the count promised.
  test('UAQ3: the incident badge opens the pre-filtered Deploys view', async ({
    page,
    skipper,
  }) => {
    await rollBackThenRetry(page, skipper);

    const badge = page.locator('[data-testid="incident-badge"]');
    await expect(badge).toBeVisible();
    await expect(page.locator('[data-testid="incident-badge-count"]')).toHaveText('1');

    // Present in every view: still there on Stacks, and its click leaves it.
    await page.locator('[data-testid="view-toggle"] button[data-view="stacks"]').click();
    await expect(badge).toBeVisible();
    await badge.click();

    // Landed on Deploys, bar revealed, the bad-outcome chips pre-selected.
    await expect(page.locator('[data-testid="deploys-table"]')).toBeVisible();
    await expect(page.locator('[data-testid="deploy-filter-wrap"]')).toHaveClass(/revealed/);
    await expect(statusChip(page, 'rolled_back')).toHaveAttribute('aria-pressed', 'true');
    await expect(statusChip(page, 'failed')).toHaveAttribute('aria-pressed', 'true');
    await expect(deployRow(page, 'rolled_back')).toBeVisible();
    await expect(deployRow(page, 'success').first()).toBeHidden();
  });

  // UAQ4 — the roster keeps the incident readable without opening anything:
  // the outcome strip shows the last outcomes as a timeline into the badge,
  // and the last-incident line names the rollback the retry papered over.
  test('UAQ4: outcome strip and last-incident line on the roster row', async ({
    page,
    skipper,
  }) => {
    await rollBackThenRetry(page, skipper);

    await page.locator('[data-testid="view-toggle"] button[data-view="stacks"]').click();
    const row = page.locator('[data-testid="roster-row"][data-stack="web"]');
    await expect(row).toBeVisible();

    // Oldest → newest, reading into the badge: success, rolled_back, success.
    const dots = row.locator('[data-testid="outcome-dot"]');
    await expect(dots).toHaveCount(3);
    await expect(dots.nth(0)).toHaveAttribute('data-status', 'success');
    await expect(dots.nth(1)).toHaveAttribute('data-status', 'rolled_back');
    await expect(dots.nth(2)).toHaveAttribute('data-status', 'success');

    const incident = row.locator('[data-testid="last-incident"]');
    await expect(incident).toBeVisible();
    await expect(incident).toContainText('rolled back');
  });

  // UAQ5 — the Logs severity chips are thresholds and classify narrated
  // outcome lines by outcome: the WARN-level "rolled back" outcome line stays
  // visible under the errors chip — hiding it there is the exact 2026-08-05
  // failure mode.
  test('UAQ5: the errors chip keeps the rollback outcome line', async ({ page, skipper }) => {
    await rollBackThenRetry(page, skipper);

    await page.locator('[data-testid="view-toggle"] button[data-view="logs"]').click();
    // The rollback outcome: WARN on the record, errors-tier for the filter.
    const outcome = page.locator('.log-line[data-level="WARN"][data-sev="ERROR"]');
    await expect(outcome.first()).toBeVisible();
    await expect(outcome.first()).toContainText('rolled back');

    await page.locator('[data-testid="log-sev-ERROR"]').click();
    await expect(outcome.first()).not.toHaveClass(/log-out/);
    await expect(outcome.first()).toBeVisible();
    // An ordinary INFO line is narrowed away — the chip still filters.
    const info = page.locator('.log-line[data-sev="INFO"]');
    await expect(info.first()).toHaveClass(/log-out/);
  });
});
