import { test, expect } from '../fixtures/test';
import type { Page } from '@playwright/test';

// Maske AG: First-run header tour (T3.15). See dev-docs/e2e-tests.md §4.34.
//
// The header is glyph-only by design, so learnability rested entirely on hover
// tooltips (which touch users never get). On a fresh browser only, a caption
// sits under each control and a dismiss banner explains it; "Got it" or Esc
// marks it seen for good (localStorage headerTourSeen), and every returning
// browser gets the clean glyph header with no flash. The whole thing is
// localStorage-gated — no timers — so shown/dismissed are deterministic.

// Opt out of the global seed: this spec exercises the first-run tour itself, so
// it must land on a genuinely fresh (unseeded) browser.
test.use({ startOptions: { stacks: ['web', 'api'] }, seedTourSeen: false });

const banner = (page: Page) => page.locator('[data-testid="header-tour"]');
const dismiss = (page: Page) => page.locator('[data-testid="header-tour-dismiss"]');
const caption = (page: Page, text: string) => page.locator('.ht-lbl', { hasText: text });
const seen = (page: Page) => page.evaluate(() => localStorage.getItem('headerTourSeen'));

// UAG1 — A fresh browser lands on the tour: the banner and the per-control
// captions are shown, and <html> is not yet marked seen.
test('UAG1: a fresh browser shows the banner and control captions', async ({ page, skipper }) => {
  await page.goto(`${skipper.baseURL}/`);
  await expect(page.locator('[data-testid="deploy-row"]')).toHaveCount(2);

  await expect(banner(page)).toBeVisible();
  // Captions naming the frequently-consulted controls are visible during the
  // tour. The view switch is not among them: its buttons carry their own
  // labels at this width, so a caption would say the same word twice.
  await expect(caption(page, 'Autosync')).toBeVisible();
  await expect(caption(page, 'Theme')).toBeVisible();
  await expect(page.locator('.ht-lbl', { hasText: 'Deploys' })).toHaveCount(0);
  await expect(page.locator('html')).not.toHaveClass(/\bheader-tour-seen\b/);
});

// UAG2 — "Got it" ends the tour, persists the choice, and it stays gone across a
// reload (the pre-paint class re-applies from localStorage).
test('UAG2: Got it dismisses the tour and it stays gone after reload', async ({
  page,
  skipper,
}) => {
  await page.goto(`${skipper.baseURL}/`);
  await expect(banner(page)).toBeVisible();

  await dismiss(page).click();
  await expect(banner(page)).toBeHidden();
  await expect(caption(page, 'Autosync')).toBeHidden();
  await expect(page.locator('html')).toHaveClass(/\bheader-tour-seen\b/);
  expect(await seen(page)).toBe('1');

  // Persisted: a reload of the same context never re-shows the tour.
  await page.reload();
  await expect(page.locator('[data-testid="deploy-row"]')).toHaveCount(2);
  await expect(banner(page)).toBeHidden();
  await expect(caption(page, 'Autosync')).toBeHidden();
});

// UAG3 — Esc is an equivalent dismiss, so a keyboard user is never trapped by
// the banner, and focus lands back on the active view control.
test('UAG3: Esc dismisses the tour and returns focus to the header', async ({ page, skipper }) => {
  await page.goto(`${skipper.baseURL}/`);
  await expect(banner(page)).toBeVisible();

  await page.keyboard.press('Escape');
  await expect(banner(page)).toBeHidden();
  expect(await seen(page)).toBe('1');
  // Focus is moved to the active view button (the dismissed banner button is gone).
  await expect(page.locator('[data-testid="view-toggle"] button.active')).toBeFocused();
});

// UAG4 — A returning browser (headerTourSeen already set) never shows the tour,
// not even for a frame: the pre-paint class is applied before the header paints.
test('UAG4: a returning browser never shows the tour', async ({ page, skipper }) => {
  await page.addInitScript(() => localStorage.setItem('headerTourSeen', '1'));
  await page.goto(`${skipper.baseURL}/`);
  await expect(page.locator('[data-testid="deploy-row"]')).toHaveCount(2);

  await expect(page.locator('html')).toHaveClass(/\bheader-tour-seen\b/);
  await expect(banner(page)).toBeHidden();
  await expect(caption(page, 'Autosync')).toBeHidden();
});

// UAG5 — On the compact ≤700px header the captions don't fit, so the tour is
// suppressed entirely (no floating banner naming captions nobody can see) even
// on a fresh browser. A phone-only visitor is never marked seen, so a later
// wider viewport still greets them.
test('UAG5: the tour is suppressed on the compact mobile header', async ({ page, skipper }) => {
  await page.setViewportSize({ width: 375, height: 720 });
  await page.goto(`${skipper.baseURL}/`);
  await expect(page.locator('[data-testid="deploy-row"]')).toHaveCount(2);

  // Fresh browser, but neither the banner nor the captions show at this width.
  await expect(banner(page)).toBeHidden();
  await expect(caption(page, 'Autosync')).toBeHidden();
  // Not dismissed — a phone visitor stays un-seen for their first desktop visit.
  await expect(page.locator('html')).not.toHaveClass(/\bheader-tour-seen\b/);
  expect(await seen(page)).toBeNull();

  // Widening the viewport reveals the tour on the same fresh session.
  await page.setViewportSize({ width: 1280, height: 720 });
  await expect(banner(page)).toBeVisible();
  await expect(caption(page, 'Autosync')).toBeVisible();
});
