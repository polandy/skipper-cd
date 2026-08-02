import { test, expect, type Page } from '../fixtures/test';

// Maske AP: an unreachable deploy stream says so, and retries in place.
// See dev-docs/e2e-tests.md §4.43.
//
// The loading skeleton deliberately does not settle on a failed connection, so
// a transient outage never reads as "no deployments" (Maske AH). Left alone
// that turns into promising forever: a page that cannot reach the server at all
// — an installed PWA opened off the network, which the service worker still
// paints from its cached shell — sat on `Connecting to deployment stream…`
// with no way out, reloading included. This pins the honest end of that: the
// skeleton gives up its spinner and shimmer rows for the load-error line, whose
// Retry reconnects without a reload.
const loadingState = (page: Page) => page.locator('[data-testid="loading-state"]');
const loadError = (page: Page) => page.locator('[data-testid="load-error"]');
const loadRetry = (page: Page) => page.locator('[data-testid="load-retry"]');
const deploysTable = (page: Page) => page.locator('[data-testid="deploys-table"]');
const connIndicator = (page: Page) => page.locator('[data-testid="conn-indicator"]');

// A stream answered with a non-2xx is fatal to EventSource — it closes for good
// rather than retrying by itself — so the page is driven purely by its own
// retry, which is the path under test. `unreachable` is flipped to false to let
// the next attempt through, standing in for a server that came back.
async function serveStream(page: Page, state: { unreachable: boolean }) {
  await page.route('**/api/events', (route) =>
    state.unreachable ? route.fulfill({ status: 503, body: 'unreachable' }) : route.continue(),
  );
}

test.describe('UAP: an unreachable deploy stream', () => {
  test('UAP1: stops promising a connection and offers a retry', async ({ page, skipper }) => {
    const state = { unreachable: true };
    await serveStream(page, state);
    await page.goto(`${skipper.baseURL}/`);

    // The notice appearing IS the settled outcome — it is rendered on the failure
    // that crosses the threshold, so the assertion cannot land early.
    await expect(loadError(page)).toBeVisible();
    await expect(loadError(page)).toContainText(
      "Can't reach skipper — the deploy stream is offline.",
    );

    // The spinner and shimmer rows both mean "rows are on their way". With the
    // notice up they must be gone, or the page still promises what it cannot
    // deliver — the whole point of the mask.
    await expect(loadingState(page).locator('.sk-status')).toBeHidden();
    await expect(loadingState(page).locator('.sk-row').first()).toBeHidden();

    // The genuine-empty state is NOT what an unreachable server means.
    await expect(page.locator('[data-testid="empty-state"]')).toBeHidden();
    await expect(connIndicator(page)).toHaveAttribute('data-state', 'reconnecting');
  });

  test('UAP2: the retry reconnects in place, with no reload', async ({ page, skipper }) => {
    const state = { unreachable: true };
    await serveStream(page, state);
    await page.goto(`${skipper.baseURL}/`);
    await expect(loadError(page)).toBeVisible();

    // Pin the document instance: the fix is that the page recovers *itself*.
    // If Retry ever became a reload, this marker would not survive it and the
    // test would fail rather than quietly passing on a fresh document.
    await page.evaluate(() => {
      (window as unknown as { __uapSameDocument: boolean }).__uapSameDocument = true;
    });

    state.unreachable = false;
    await loadRetry(page).click();

    await expect(deploysTable(page)).toBeVisible();
    await expect(connIndicator(page)).toHaveAttribute('data-state', 'connected');
    // The notice is retracted only by a connection that came up.
    await expect(loadError(page)).toBeHidden();
    expect(
      await page.evaluate(
        () => (window as unknown as { __uapSameDocument?: boolean }).__uapSameDocument === true,
      ),
    ).toBe(true);
  });

  test('UAP3: a reachable server never shows the notice', async ({ page, skipper }) => {
    // The counter-check: without it UAP1 would still pass if the notice were
    // rendered unconditionally, which would be a worse bug than the one fixed.
    await page.goto(`${skipper.baseURL}/`);
    await expect(deploysTable(page)).toBeVisible();
    await expect(connIndicator(page)).toHaveAttribute('data-state', 'connected');
    await expect(loadError(page)).toBeHidden();
  });
});
