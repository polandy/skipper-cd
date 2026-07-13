import { test, expect } from '../fixtures/test';
import { buildCommit, manifestVersion } from '../fixtures/harness';
import { visualSnapshot } from '../fixtures/snapshot';

// Deploy rows carry a relative time and a duration that vary run-to-run; mask
// them out of any full-page screenshot (docs/e2e-tests.md §5).
const pageMasks = (page: import('@playwright/test').Page) => [
  page.locator('[data-testid="time-cell"]'),
  page.locator('[data-testid="duration-cell"]'),
];

// Maske D: Global chrome. See docs/e2e-tests.md §4.5.

// UD5 — Build-identity label. globalSetup builds the binary with the version and
// commit injected via -ldflags but no branch — the official-release path. A
// release is named uniquely by its semver, so the header shows `v<semver>` and
// deliberately omits the commit (the hash is only appended for non-release
// builds: feature branches and local dev). This asserts the full through-line
// (ldflags → /api/version → header render) and that the commit is suppressed.
test('UD5: header shows the release build identity without the commit', async ({ page, skipper }) => {
  await page.goto(`${skipper.baseURL}/`);

  const label = page.locator('[data-testid="brand-version"]');
  await expect(label).toHaveText(`v${manifestVersion()}`);
  await expect(label).not.toContainText(buildCommit);
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

    // Snapshot: the compact mobile layout (§5 anchor), dynamic cells masked.
    await visualSnapshot(page, 'mobile-layout.png', { mask: pageMasks(page) });

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

// UD1 — Theme toggle + no-flash. `theme-toggle` switches Catppuccin Mocha↔Latte
// and persists the choice in `localStorage theme`. After a reload the inline
// head script applies the `latte` class before first paint, so the light theme
// is present immediately with no flash of the dark default.
test('UD1: theme toggle switches Mocha↔Latte, persists, and applies before paint', async ({
  page,
  skipper,
}) => {
  const themeToggle = page.locator('[data-testid="theme-toggle"]');
  const hasLatte = () => page.evaluate(() => document.documentElement.classList.contains('latte'));
  const storedTheme = () => page.evaluate(() => localStorage.getItem('theme'));

  await page.goto(`${skipper.baseURL}/`);
  // The startup success row is present, so both theme snapshots have real content.
  await expect(page.locator('[data-testid="deploy-row"]')).toHaveCount(1);

  // Default is Mocha (dark): no `latte` class.
  expect(await hasLatte()).toBe(false);
  // Snapshot: the dark theme (§5 anchor).
  await visualSnapshot(page, 'theme-mocha.png', { mask: pageMasks(page) });

  // Toggle → Latte, and the choice is persisted.
  await themeToggle.click();
  expect(await hasLatte()).toBe(true);
  expect(await storedTheme()).toBe('latte');
  // Snapshot: the light theme (§5 anchor).
  await visualSnapshot(page, 'theme-latte.png', { mask: pageMasks(page) });

  // Reload: the head script re-applies `latte` before first paint, so the class
  // is present immediately (no flash) without any interaction.
  await page.reload();
  expect(await hasLatte()).toBe(true);

  // Toggle back → Mocha, persisted.
  await themeToggle.click();
  expect(await hasLatte()).toBe(false);
  expect(await storedTheme()).toBe('mocha');
});

// UD2 — Connection indicator. `conn-indicator` exposes its state as `data-state`.
// It reaches `connected` once the SSE stream opens; killing the backend drops the
// stream so it flips to `reconnecting`; bringing the binary back on the same port
// lets EventSource auto-reconnect and it returns to `connected`.
test('UD2: connection indicator tracks connected→reconnecting→connected', async ({
  page,
  skipper,
}) => {
  const conn = page.locator('[data-testid="conn-indicator"]');

  await page.goto(`${skipper.baseURL}/`);
  await expect(conn).toHaveAttribute('data-state', 'connected');

  // Kill the backend: the SSE stream errors and the indicator flips.
  await skipper.stop();
  await expect(conn).toHaveAttribute('data-state', 'reconnecting');

  // Bring it back on the same port → EventSource auto-reconnects.
  await skipper.relaunch();
  await expect(conn).toHaveAttribute('data-state', 'connected', { timeout: 15000 });
});

// UD3 — Deploy indicator. `deploy-indicator` names the actively deploying
// stack(s) while a deploy is in flight and reads `idle` otherwise. The state is
// mirrored into `aria-label` (so it survives the span being hidden on mobile),
// which is what we assert across a held→released deploy.
test('UD3: deploy indicator names the active stack while held, idle otherwise', async ({
  page,
  skipper,
}) => {
  const indicator = page.locator('[data-testid="deploy-indicator"]');

  await page.goto(`${skipper.baseURL}/`);
  await expect(indicator).toContainText('idle'); // nothing deploying at rest

  // Hold the next `up` and push a change → the stack stays deploying and the
  // indicator names it.
  skipper.hold();
  skipper.setStackImage('web', '1.26');
  expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);
  await expect(indicator).toHaveAttribute('aria-label', 'deploying web');

  // Release → the deploy completes and the indicator returns to idle.
  skipper.release();
  await expect(indicator).toHaveAttribute('aria-label', 'idle');
});
