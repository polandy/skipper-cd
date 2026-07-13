import { test, expect } from '../fixtures/test';
import { buildCommit, manifestVersion } from '../fixtures/harness';

// Maske D: Global chrome. See docs/e2e-tests.md §4.5.

// UD5 — Build-identity label. The header shows the deployed build as
// `v<semver> · <commit>`. globalSetup builds the binary with the version and
// commit injected via -ldflags (the same source the Docker/Nix builds use, no
// branch → the version path), so this asserts the full through-line: ldflags →
// /api/version → header render, against the exact build that ships.
test('UD5: header shows the deployed build identity', async ({ page, skipper }) => {
  await page.goto(`${skipper.baseURL}/`);

  const label = page.locator('[data-testid="brand-version"]');
  await expect(label).toHaveText(`v${manifestVersion()} · ${buildCommit}`);
});

// UD4 — Responsive ≤700px (compact header + table collapse). One test covering
// the whole mobile contract (UI_SPEC §Responsive): the header fits a narrow
// viewport without horizontal scroll and drops the `skipper-cd` wordmark, and
// the deploy table collapses so the Files pill hides while tapping a row with
// changed files still expands the panel. The 1280px pass is the control
// (wordmark + pill visible, likewise no sideways scroll). The `web` startup
// deploy already lands a success row with changed files, so no webhook is
// needed (same fixture as UA7).
test.describe('UD4: responsive ≤700px', () => {
  type Page = import('@playwright/test').Page;
  const wordmark = (page: Page) => page.locator('[data-testid="brand-name"]');
  const successRow = (page: Page) =>
    page.locator('[data-testid="deploy-row"][data-stack="web"][data-status="success"]');
  const filesPill = (page: Page) => successRow(page).locator('[data-testid="files-pill"]');
  const filesPanel = (page: Page) => page.locator('[data-testid="files-panel"]');
  const fitsViewport = (page: Page) =>
    page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth);

  test('compact header fits, wordmark drops, and tapping a row expands its files', async ({ page, skipper }) => {
    // Desktop control: wordmark and Files pill visible, no sideways scroll.
    await page.setViewportSize({ width: 1280, height: 800 });
    await page.goto(`${skipper.baseURL}/`);
    await expect(successRow(page)).toHaveCount(1); // startup settled
    await expect(wordmark(page)).toBeVisible();
    await expect(filesPill(page)).toBeVisible();
    expect(await fitsViewport(page)).toBe(true);

    // Mobile: the header stays within the viewport and the wordmark is gone.
    await page.setViewportSize({ width: 390, height: 844 });
    await expect(wordmark(page)).toBeHidden();
    expect(await fitsViewport(page)).toBe(true);

    // Table collapses: the Files pill is hidden, yet tapping the row still
    // toggles the files panel directly below it (open, then close).
    await expect(filesPill(page)).toBeHidden();
    await expect(filesPanel(page)).toHaveCount(0);
    await successRow(page).click();
    await expect(filesPanel(page)).toBeVisible();
    const siblingTestid = await successRow(page).evaluate(
      (row) => row.nextElementSibling?.getAttribute('data-testid') ?? null,
    );
    expect(siblingTestid).toBe('files-panel');
    await successRow(page).click();
    await expect(filesPanel(page)).toHaveCount(0);
  });

  // The four header filter-toggles lose their label on mobile; a bare switch
  // track is then indistinguishable (and touch shows no tooltip), so each swaps
  // its track for a self-describing glyph. Assert the swap on both a
  // deploys-view toggle (time-mode) and a logs-view toggle (sort/follow), and
  // that the theme glyph reflects the current mode (moon dark → sun light).
  test('unlabelled toggles swap their track for a per-toggle glyph on mobile', async ({ page, skipper }) => {
    const track = (id: string) => page.locator(`[data-testid="${id}"] .toggle-track`);
    const glyph = (id: string) => page.locator(`[data-testid="${id}"] .tg-ico`);

    await page.goto(`${skipper.baseURL}/`);

    // Desktop control: the labelled switch track shows, the glyph stays hidden.
    await page.setViewportSize({ width: 1280, height: 800 });
    await expect(track('time-mode')).toBeVisible();
    await expect(glyph('time-mode')).toBeHidden();
    await expect(track('theme-toggle')).toBeVisible();
    await expect(glyph('theme-toggle')).toBeHidden();

    // Mobile: the track (and label) give way to the glyph.
    await page.setViewportSize({ width: 390, height: 844 });
    await expect(track('time-mode')).toBeHidden();
    await expect(glyph('time-mode')).toBeVisible();
    await expect(track('theme-toggle')).toBeHidden();
    await expect(glyph('theme-toggle')).toBeVisible();

    // The theme glyph reflects the mode: moon in dark (default), sun once
    // toggled to light. This is the state-driven variant the others don't have.
    const themeGlyph = glyph('theme-toggle');
    await expect(themeGlyph.locator('.tg-moon')).toBeVisible();
    await expect(themeGlyph.locator('.tg-sun')).toBeHidden();
    await page.locator('[data-testid="theme-toggle"]').click();
    await expect(themeGlyph.locator('.tg-sun')).toBeVisible();
    await expect(themeGlyph.locator('.tg-moon')).toBeHidden();
    await page.locator('[data-testid="theme-toggle"]').click(); // restore dark

    // The logs-only toggles use the same swap — switch views and confirm both.
    await page.locator('[data-testid="view-toggle"] button[data-view="logs"]').click();
    await expect(track('log-sort')).toBeHidden();
    await expect(glyph('log-sort')).toBeVisible();
    await expect(track('follow-logs')).toBeHidden();
    await expect(glyph('follow-logs')).toBeVisible();
  });
});
