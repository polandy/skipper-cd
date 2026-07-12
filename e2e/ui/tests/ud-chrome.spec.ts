import { test, expect } from '../fixtures/test';
import { manifestVersion } from '../fixtures/harness';

// Maske D: Global chrome. See docs/e2e-tests.md §4.5.

// UD5 — Version label. The header shows the deployed skipper-cd version as
// `v<semver>`. globalSetup builds the binary with the version injected via
// -ldflags from .release-please-manifest.json (the same source the Docker/Nix
// builds use), so this asserts the full through-line: ldflags → /api/version →
// header render, against the exact version that ships.
test('UD5: header shows the deployed version', async ({ page, skipper }) => {
  await page.goto(`${skipper.baseURL}/`);

  const label = page.locator('[data-testid="brand-version"]');
  await expect(label).toHaveText(`v${manifestVersion()}`);
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
});
