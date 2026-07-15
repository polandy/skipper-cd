import { test, expect } from '../fixtures/test';
import type { Page } from '@playwright/test';

// Maske H: Web UI auth — the login overlay (ADR-0028). See docs/e2e-tests.md §4.9.
//
// The server's auth gate protects /api/*; the app shell stays open so the login
// overlay can render on a 401. Behaviour-only (no visual snapshots): the idle
// header is unchanged, so the existing baselines still hold.

const overlay = (page: Page) => page.locator('[data-testid="auth-overlay"]');
const form = (page: Page) => page.locator('[data-testid="auth-form"]');
const tokenInput = (page: Page) => page.locator('[data-testid="auth-token"]');
const submit = (page: Page) => page.locator('[data-testid="auth-submit"]');
const errorMsg = (page: Page) => page.locator('[data-testid="auth-error"]');
const note = (page: Page) => page.locator('[data-testid="auth-note"]');
const rows = (page: Page) => page.locator('[data-testid="deploy-row"]');
const signout = (page: Page) => page.locator('[data-testid="vo-signout"]');
const deploysBtn = (page: Page) =>
  page.locator('[data-testid="view-toggle"] button[data-view="deploys"]');

// UH1 — Token login. With a token configured, opening the UI shows the login
// overlay instead of the app and the event stream is not opened. A wrong token
// gives an inline error; the right token validates, persists the cookie, and
// reloads onto the app. Signing out from the popover clears it and returns to
// the overlay.
test.describe('token auth', () => {
  test.use({ startOptions: { stacks: ['web'], auth: { token: 's3cret-e2e' } } });

  test('UH1: wrong token errors, right token unlocks, sign-out returns to the overlay', async ({
    page,
    skipper,
  }) => {
    await page.goto(`${skipper.baseURL}/`);

    // Gated: the login overlay covers the app, offering the token form. The
    // event stream is never opened, so no deploy rows render behind it.
    await expect(overlay(page)).toBeVisible();
    await expect(form(page)).toBeVisible();
    await expect(rows(page)).toHaveCount(0);

    // Wrong token: inline error, still on the overlay (no reload).
    await tokenInput(page).fill('nope');
    await submit(page).click();
    await expect(errorMsg(page)).toBeVisible();
    await expect(overlay(page)).toBeVisible();

    // Right token: validated via the bearer probe, cookie stored, reload onto
    // the app — the startup deploy row now streams in.
    await tokenInput(page).fill('s3cret-e2e');
    await submit(page).click();
    await expect(overlay(page)).toBeHidden();
    await expect(rows(page)).toHaveCount(1);

    // The token was persisted in the skipper_auth cookie.
    const cookies = await page.context().cookies();
    expect(cookies.some((c) => c.name === 'skipper_auth' && c.value === 's3cret-e2e')).toBe(true);

    // Sign out from the view-options popover → cookie cleared, overlay returns.
    await deploysBtn(page).click();
    await expect(signout(page)).toBeVisible();
    await signout(page).click();
    await expect(overlay(page)).toBeVisible();
    await expect(form(page)).toBeVisible();
  });
});

// UH2 — Proxy-only. With only a trusted header + proxies configured, a direct
// browser (not an allowlisted upstream, carrying no header) is denied: the
// overlay shows the access-denied note and offers no token field.
test.describe('proxy-only auth', () => {
  test.use({
    startOptions: {
      stacks: ['web'],
      auth: { trustedHeader: 'Remote-User', trustedProxies: ['10.255.255.255/32'] },
    },
  });

  test('UH2: an untrusted client sees the access-denied note and no token field', async ({
    page,
    skipper,
  }) => {
    await page.goto(`${skipper.baseURL}/`);
    await expect(overlay(page)).toBeVisible();
    await expect(note(page)).toBeVisible();
    await expect(form(page)).toBeHidden();
    await expect(rows(page)).toHaveCount(0);
  });
});
