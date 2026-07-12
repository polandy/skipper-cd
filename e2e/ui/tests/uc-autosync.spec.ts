import { test, expect } from '../fixtures/test';
import type { Page } from '@playwright/test';

// Maske C: Autosync-Drawer. See docs/e2e-tests.md §4.4.

// UC1 — Header state. The autosync header control (`autosync-btn`) shows the
// *global* autosync state, and it is driven by the live `autosync` SSE event —
// not a `localStorage` preference. The button exposes that state as
// `data-global` ("true"/"false"), the machine-readable twin of its amber
// "paused" styling. We flip global autosync through the real API
// (`POST /api/autosync`) from a separate client, so the browser can only learn
// of the change via the server's broadcast `autosync` event; the header must
// mirror it live and again after a reload (proving it is server state, never
// persisted locally).
test.describe('UC1: header state', () => {
  const autosyncBtn = (page: Page) => page.locator('[data-testid="autosync-btn"]');

  test('the header control mirrors global autosync from the SSE event, across reload', async ({
    page,
    skipper,
  }) => {
    await page.goto(`${skipper.baseURL}/`);

    // Default config has global autosync on; the button reflects it once the
    // initial `autosync` snapshot streams in over SSE.
    await expect(autosyncBtn(page)).toHaveAttribute('data-global', 'true');

    // Pause globally via the real API (a separate client): the server publishes
    // an `autosync` event and the header flips live — no reload.
    expect(await skipper.postAutosync('', false)).toBe(200);
    await expect(autosyncBtn(page)).toHaveAttribute('data-global', 'false');

    // It is server state, not a `localStorage` toggle: a reload restores the
    // paused state from the server's snapshot, not from the browser.
    await page.reload();
    await expect(autosyncBtn(page)).toHaveAttribute('data-global', 'false');

    // Resuming flips it back on, live.
    expect(await skipper.postAutosync('', true)).toBe(200);
    await expect(autosyncBtn(page)).toHaveAttribute('data-global', 'true');
  });
});

// UC2 — Pending pill. The amber `pending-pill` on the header control is hidden
// when nothing is queued. Pausing autosync and then pushing a change defers the
// paused stack: the server registers it as pending and broadcasts a `queue`
// event, which makes the pill appear carrying the queue count. Resuming autosync
// drains the queue, so the registry empties and the pill hides again — the pill
// tracks live server queue depth, not a client guess.
test.describe('UC2: pending pill', () => {
  const pendingPill = (page: Page) => page.locator('[data-testid="pending-pill"]');

  test('the pending pill shows the queued count while paused and hides on drain', async ({
    page,
    skipper,
  }) => {
    await page.goto(`${skipper.baseURL}/`);

    // Nothing is queued at boot, so the pill is hidden.
    await expect(pendingPill(page)).toBeHidden();

    // Pause globally, then push a change: the paused `web` stack is deferred and
    // the pending registry (broadcast as a `queue` event) makes the pill appear
    // with the count.
    expect(await skipper.postAutosync('', false)).toBe(200);
    skipper.setStackImage('web', '1.26');
    expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);
    await expect(pendingPill(page)).toBeVisible();
    await expect(pendingPill(page)).toHaveText('1');

    // Resuming autosync drains the queue; the registry empties and the pill hides.
    expect(await skipper.postAutosync('', true)).toBe(200);
    await expect(pendingPill(page)).toBeHidden();
  });
});
