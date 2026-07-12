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
