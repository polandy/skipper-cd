import { test, expect } from '../fixtures/test';
import type { Page } from '@playwright/test';
import type { Skipper } from '../fixtures/harness';

// Maske F: Upcoming-deploys look-ahead. See docs/e2e-tests.md §4.7.
//
// A multi-stack run is held on the first stack's `up`, so the run sits with one
// stack deploying and the rest still to come — exactly the state the look-ahead
// surfaces. The three stacks are pushed together, so the run plan is the full
// set in config order (web → api → db) and, while web is held, the `upcoming`
// snapshot is [api, db]. Everything is driven through the real backend (the
// `upcoming` SSE event is emitted by skipper itself), never faked.

test.use({ startOptions: { stacks: ['web', 'api', 'db'] } });

const indicator = (page: Page) => page.locator('[data-testid="deploy-indicator"]');
const runDrawer = (page: Page) => page.locator('[data-testid="run-drawer"]');

// heldRun pushes a change to all three stacks under a held `up`, so the run
// blocks with `web` deploying and [api, db] upcoming. Returns once the indicator
// reflects that state.
async function heldRun(page: Page, skipper: Skipper) {
  skipper.hold();
  skipper.setStackImage('web', '1.26');
  skipper.setStackImage('api', '1.26');
  skipper.setStackImage('db', '1.26');
  expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);
  await expect(indicator(page)).toHaveAttribute('aria-label', 'deploying web · next api, db');
}

// UF1 — Look-ahead trail. While a run is held on its first stack, the indicator
// names the active stack and the stacks still to come this run. The full state
// lives in `aria-label` (mirrored so it survives the trail being hidden on
// mobile), and the visible `deploy-next` trail names the upcoming stacks.
test('UF1: the indicator shows the active stack and the upcoming trail', async ({
  page,
  skipper,
}) => {
  await page.goto(`${skipper.baseURL}/`);
  await expect(indicator(page)).toHaveAttribute('aria-label', 'idle');

  await heldRun(page, skipper);

  const trail = page.locator('[data-testid="deploy-next"]');
  await expect(trail).toBeVisible();
  await expect(trail).toContainText('api');
  await expect(trail).toContainText('db');

  // Releasing drains the run; the trail empties and the indicator returns to idle.
  skipper.release();
  await expect(indicator(page)).toHaveAttribute('aria-label', 'idle');
  await expect(trail).toBeHidden();
});

// UF2 — Run panel. Clicking the indicator while a run is active opens a
// read-only panel listing the run in deploy order: the active stack first
// ("deploying now"), then the upcoming stacks. It closes when the run ends.
test('UF2: clicking the indicator opens the run panel with the ordered run', async ({
  page,
  skipper,
}) => {
  await page.goto(`${skipper.baseURL}/`);

  // Idle: the panel is not openable (nothing to show).
  await indicator(page).click();
  await expect(runDrawer(page)).not.toHaveClass(/open/);

  await heldRun(page, skipper);

  await indicator(page).click();
  await expect(runDrawer(page)).toHaveClass(/open/);

  // The active stack leads, tagged "deploying now"; the rest follow in order.
  const webRow = runDrawer(page).locator('.run-row[data-stack="web"]');
  await expect(webRow).toHaveClass(/active/);
  await expect(webRow).toContainText('deploying now');
  await expect(runDrawer(page).locator('.run-row[data-stack="api"]')).toContainText('next');
  await expect(runDrawer(page).locator('.run-row[data-stack="db"]')).toBeVisible();

  // The run ending closes the panel automatically.
  skipper.release();
  await expect(indicator(page)).toHaveAttribute('aria-label', 'idle');
  await expect(runDrawer(page)).not.toHaveClass(/open/);
});

// UF3 — Mobile fallback. On a narrow viewport the header is glyph-only: the
// active name and the trail are dropped, and the upcoming stacks collapse to a
// compact `+N` count chip (the names stay reachable via the panel and aria).
test('UF3: the trail collapses to a +N count chip on mobile', async ({ page, skipper }) => {
  await page.setViewportSize({ width: 390, height: 800 });
  await page.goto(`${skipper.baseURL}/`);

  await heldRun(page, skipper);

  const count = page.locator('[data-testid="deploy-count"]');
  await expect(count).toBeVisible();
  await expect(count).toHaveText('+2');
  // The verbose trail is hidden on mobile; the names remain in aria-label.
  await expect(page.locator('[data-testid="deploy-next"]')).toBeHidden();

  skipper.release();
  await expect(indicator(page)).toHaveAttribute('aria-label', 'idle');
  await expect(count).toBeHidden();
});
