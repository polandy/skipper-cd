import { test, expect } from '../fixtures/test';
import { buildCommit, buildVersion } from '../fixtures/harness';
import { visualSnapshot } from '../fixtures/snapshot';

// Deploy rows carry a relative time and a duration that vary run-to-run; mask
// them out of any full-page screenshot (dev-docs/e2e-tests.md §5). The header
// version label is stable — globalSetup injects a fixed buildVersion/buildCommit
// — so it needs no mask.
const pageMasks = (page: import('@playwright/test').Page) => [
  page.locator('[data-testid="time-cell"]'),
  page.locator('[data-testid="duration-cell"]'),
];

// Human-readable theme labels, mirroring THEME_LABELS in the UI. The mismatch
// notice names themes by these labels, not their raw config values, so UD6
// asserts against the label of the configured theme.
const THEME_LABELS: Record<string, string> = {
  catppuccin: 'Catppuccin',
  nord: 'Nord',
  solarized: 'Solarized',
  gruvbox: 'Gruvbox',
  'rose-pine': 'Rosé Pine',
  flake: 'Flake',
};

// Maske D: Global chrome. See dev-docs/e2e-tests.md §4.5.

// UD5 — Build-identity label. The header shows the deployed build as
// `v<semver> · <commit>`. globalSetup builds the binary with the version and
// commit injected via -ldflags (the same source the Docker/Nix builds use, no
// branch → the version path), so this asserts the full through-line: ldflags →
// /api/version → header render, against the exact build that ships.
test('UD5: header shows the deployed build identity', async ({ page, skipper }) => {
  await page.goto(`${skipper.baseURL}/`);

  const label = page.locator('[data-testid="brand-version"]');
  await expect(label).toHaveText(`v${buildVersion} · ${buildCommit}`);
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

});

// UD8 — View-options popover. The view-specific toggles (currently: deploys'
// time mode) live in a popover opened from the *active* view button, not the
// header row, so switching views never makes a header control appear or
// disappear. Logs carries no group — its search/wrap/auto-scroll/fullscreen
// tools live inline in its own panel header instead (see UB8), so clicking
// the active logs button is a no-op here.
test('UD8: view-specific options live in a popover opened from the active view button', async ({
  page,
  skipper,
}) => {
  const options = page.locator('[data-testid="view-options"]');
  const timeMode = page.locator('[data-testid="time-mode"]');
  const deploysBtn = page.locator('[data-testid="view-toggle"] button[data-view="deploys"]');
  const logsBtn = page.locator('[data-testid="view-toggle"] button[data-view="logs"]');

  await page.goto(`${skipper.baseURL}/`);

  // Closed by default: no view-specific toggle sits in the header.
  await expect(options).toBeHidden();
  await expect(timeMode).toBeHidden();

  // Deploys is active: clicking its button opens the popover showing the
  // deploys group (time mode).
  await deploysBtn.click();
  await expect(options).toBeVisible();
  await expect(timeMode).toBeVisible();

  // The toggle works from inside the popover, and Esc closes it.
  await expect(timeMode).not.toHaveClass(/\bactive\b/);
  await timeMode.click();
  await expect(timeMode).toHaveClass(/\bactive\b/);
  await page.keyboard.press('Escape');
  await expect(options).toBeHidden();

  // Switching to logs surfaces no header control and opens no popover; a
  // second click on the now-active logs button stays a no-op, since the view
  // carries no vo-group.
  await logsBtn.click(); // deploys → logs (popover stays closed)
  await expect(options).toBeHidden();
  await logsBtn.click(); // active-button click, but nothing to open
  await expect(options).toBeHidden();

  // Back on deploys, clicking outside (the brand) closes an open popover.
  await deploysBtn.click();
  await deploysBtn.click();
  await expect(options).toBeVisible();
  await page.locator('header .brand').click();
  await expect(options).toBeHidden();
});

// UD9 — Theme glyph. The header theme toggle is glyph-only on every viewport;
// its glyph reflects the mode — a moon in dark (default), a sun in light — and
// flips when toggled.
test('UD9: the theme toggle glyph shows a moon in dark and a sun in light', async ({
  page,
  skipper,
}) => {
  const glyph = page.locator('[data-testid="theme-toggle"] .tg-ico');

  await page.goto(`${skipper.baseURL}/`);
  await expect(glyph.locator('.tg-moon')).toBeVisible();
  await expect(glyph.locator('.tg-sun')).toBeHidden();

  await page.locator('[data-testid="theme-toggle"]').click();
  await expect(glyph.locator('.tg-sun')).toBeVisible();
  await expect(glyph.locator('.tg-moon')).toBeHidden();

  await page.locator('[data-testid="theme-toggle"]').click(); // restore dark
});

// UD1 — Theme toggle + no-flash. `theme-toggle` switches the configured
// theme's dark↔light variant and persists the choice in `localStorage
// colorScheme`. After a reload the inline head script applies the `light`
// class before first paint, so the light variant is present immediately with
// no flash of the dark default.
test('UD1: theme toggle switches dark<->light, persists, and applies before paint', async ({
  page,
  skipper,
}) => {
  const themeToggle = page.locator('[data-testid="theme-toggle"]');
  const isLight = () => page.evaluate(() => document.documentElement.classList.contains('light'));
  const storedColorScheme = () => page.evaluate(() => localStorage.getItem('colorScheme'));

  await page.goto(`${skipper.baseURL}/`);
  // The startup success row is present, so both theme snapshots have real content.
  await expect(page.locator('[data-testid="deploy-row"]')).toHaveCount(1);

  // Default is dark: no `light` class.
  expect(await isLight()).toBe(false);
  // Snapshot: the dark theme (§5 anchor).
  await visualSnapshot(page, 'theme-dark.png', { mask: pageMasks(page) });

  // Toggle → light, and the choice is persisted.
  await themeToggle.click();
  expect(await isLight()).toBe(true);
  expect(await storedColorScheme()).toBe('light');
  // Snapshot: the light theme (§5 anchor).
  await visualSnapshot(page, 'theme-light.png', { mask: pageMasks(page) });

  // Reload: the head script re-applies `light` before first paint, so the class
  // is present immediately (no flash) without any interaction.
  await page.reload();
  expect(await isLight()).toBe(true);

  // Toggle back → dark, persisted.
  await themeToggle.click();
  expect(await isLight()).toBe(false);
  expect(await storedColorScheme()).toBe('dark');
});

// UD6 / UD6b exercise the opt-in theme picker, so the instance is started with
// `ui_theme_switcher` enabled. The default (switcher off) is covered by UD7.
test.describe('theme switcher enabled', () => {
  test.use({ startOptions: { stacks: ['web'], themeSwitcher: true } });

  // UD6 — Theme picker + per-browser override. `theme-select` switches the
  // active palette instantly (every theme's CSS is always present, so this is a
  // plain attribute change — no navigation, no flash). A non-default choice is
  // a local-only override: it persists across reload, survives independently of
  // the dark/light toggle, and never touches `data-server-theme` (the
  // environment's actual configured theme). Choosing the server's own theme
  // again clears the override. Whenever an override is active, a dismissible
  // notice explains the mismatch and auto-hides itself.
  test('UD6: theme picker switches live, persists a local override, and surfaces a mismatch notice', async ({
    page,
    skipper,
  }) => {
    const themeSelect = page.locator('[data-testid="theme-select"]');
    const notice = page.locator('[data-testid="theme-notice"]');
    const noticeClose = page.locator('[data-testid="theme-notice-close"]');
    const effectiveTheme = () => page.evaluate(() => document.documentElement.getAttribute('data-theme'));
    const serverTheme = () => page.evaluate(() => document.documentElement.getAttribute('data-server-theme'));
    const storedOverride = () => page.evaluate(() => localStorage.getItem('themeOverride'));

    await page.goto(`${skipper.baseURL}/`);
    const configured = await serverTheme();

    // No override yet: picker mirrors the server-configured theme, no notice.
    expect(await effectiveTheme()).toBe(configured);
    expect(await themeSelect.inputValue()).toBe(configured);
    await expect(notice).toBeHidden();

    // Pick a different theme: applies immediately (no reload), persists a local
    // override, and the mismatch notice appears without needing a page reload.
    const other = configured === 'nord' ? 'gruvbox' : 'nord';
    await themeSelect.selectOption(other);
    expect(await effectiveTheme()).toBe(other);
    expect(await storedOverride()).toBe(other);
    await expect(notice).toBeVisible();
    await expect(notice).toContainText(THEME_LABELS[configured as string]);

    // The override survives a reload (pre-paint script), independently of the
    // dark/light toggle's own localStorage key.
    await page.reload();
    expect(await effectiveTheme()).toBe(other);
    expect(await themeSelect.inputValue()).toBe(other);
    await expect(notice).toBeVisible();

    // Manually dismissing hides the notice without clearing the override.
    await noticeClose.click();
    await expect(notice).toBeHidden();
    expect(await storedOverride()).toBe(other);

    // Picking the configured theme again clears the override — the page goes
    // back to following whatever the environment is configured for.
    await themeSelect.selectOption(configured);
    expect(await effectiveTheme()).toBe(configured);
    expect(await storedOverride()).toBe(null);
    await expect(notice).toBeHidden();
  });

  // UD6b — the mismatch notice auto-hides itself. The clock is faked *before*
  // navigation so the page's own `setTimeout(…, 6000)` is captured by
  // Playwright's virtual clock instead of a real one, letting the wait be
  // instant and deterministic rather than a real 7s sleep.
  test('UD6b: theme mismatch notice auto-hides a few seconds after it appears', async ({
    page,
    skipper,
  }) => {
    await page.clock.install();

    const themeSelect = page.locator('[data-testid="theme-select"]');
    const notice = page.locator('[data-testid="theme-notice"]');

    await page.goto(`${skipper.baseURL}/`);
    const configured = await page.evaluate(() => document.documentElement.getAttribute('data-server-theme'));
    const other = configured === 'nord' ? 'gruvbox' : 'nord';

    await themeSelect.selectOption(other);
    await expect(notice).toBeVisible();

    await page.clock.fastForward('00:07'); // past the 6s auto-hide timeout
    await expect(notice).toBeHidden();
  });
});

// UD7 — Theme switcher off (the default). With `ui_theme_switcher` unset the
// picker must be absent from the UI and a saved override must stay dormant: the
// pre-paint script leaves `data-theme` on the configured theme and no mismatch
// notice appears, so a locked-down deployment keeps its at-a-glance colour.
test('UD7: with the switcher off, the picker is hidden and a saved override is ignored', async ({
  page,
  skipper,
}) => {
  const themeSelect = page.locator('[data-testid="theme-select"]');
  const notice = page.locator('[data-testid="theme-notice"]');
  const effectiveTheme = () => page.evaluate(() => document.documentElement.getAttribute('data-theme'));

  await page.goto(`${skipper.baseURL}/`);
  const configured = await page.evaluate(() => document.documentElement.getAttribute('data-server-theme'));

  // The picker is not shown, and the page is on the configured theme.
  await expect(themeSelect).toBeHidden();
  expect(await effectiveTheme()).toBe(configured);
  await expect(notice).toBeHidden();

  // Seed an override that differs from the configured theme, as if this browser
  // had one saved from a time the switcher was enabled. With the switcher off
  // the pre-paint script must ignore it: after a reload the page stays on the
  // configured theme and the notice never appears.
  const other = configured === 'nord' ? 'gruvbox' : 'nord';
  await page.evaluate((t) => localStorage.setItem('themeOverride', t), other);
  await page.reload();

  expect(await effectiveTheme()).toBe(configured);
  await expect(themeSelect).toBeHidden();
  await expect(notice).toBeHidden();
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

// UD10 — Fatal-stream recovery. UD2 covers the *transient* drop (the browser's
// own auto-reconnect brings the indicator back). This covers the *fatal* case:
// a non-2xx reconnect response puts EventSource in CLOSED, so the browser never
// retries on its own. The page must retry itself and recover, rather than
// sitting on `reconnecting` forever. We force the fatal response by intercepting
// the reconnect with a 503, then lift it so the page's own backoff reconnect
// re-establishes against the live server.
test('UD10: indicator recovers after a fatal (non-200) stream error the browser will not retry', async ({
  page,
  skipper,
}) => {
  const conn = page.locator('[data-testid="conn-indicator"]');

  await page.goto(`${skipper.baseURL}/`);
  await expect(conn).toHaveAttribute('data-state', 'connected');

  // Make every *new* /api/events request fail fatally (the already-open stream is
  // untouched). A 503 is a non-200, so EventSource closes for good on the retry.
  await page.route('**/api/events', (route) => route.fulfill({ status: 503 }));

  // Drop the live stream: the browser auto-reconnects, hits the 503, and lands in
  // CLOSED — where without the page's own retry it would stay forever.
  await skipper.stop();
  await expect(conn).toHaveAttribute('data-state', 'reconnecting');

  // Lift the fault and bring the backend back: the page's backoff reconnect must
  // re-open the stream on its own (the browser will not, having given up).
  await page.unroute('**/api/events');
  await skipper.relaunch();
  await expect(conn).toHaveAttribute('data-state', 'connected', { timeout: 15000 });
});

// UD12 — State published while the stream is connecting is not lost. The stream
// carries its own baseline (subscribe, then send current state, then deltas), so
// there is no window between "read the baseline" and "start listening" for a
// change to fall into. Forced, not awaited: the `/api/events` request is held
// before it reaches the server, a deploy is queued while nothing is subscribed,
// and only then is the request released. With the baseline fetched separately
// the queued deploy reached nobody and was never re-sent — the pending pill
// stayed blank until the next run.
test('UD12: a state change published while the stream connects still reaches the UI', async ({
  page,
  skipper,
}) => {
  let release = () => {};
  const held = new Promise<void>((resolve) => {
    release = resolve;
  });
  let markAttempted = () => {};
  const attempted = new Promise<void>((resolve) => {
    markAttempted = resolve;
  });

  // Hold only the first stream request; the page's own retries pass through.
  let holdNext = true;
  await page.route('**/api/events', async (route) => {
    if (!holdNext) return route.continue();
    holdNext = false;
    markAttempted(); // the page is connecting; the server has no subscriber yet
    await held;
    await route.continue();
  });

  page.goto(`${skipper.baseURL}/`).catch(() => {});
  await attempted;

  // Queue a deploy while nobody is listening: pause autosync, then push.
  expect(await skipper.postAutosync('', false)).toBe(200);
  skipper.setStackImage('web', '1.26');
  expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);
  // Read out-of-band (page.route does not see this) that the server really queued it.
  await expect
    .poll(async () => (await (await fetch(`${skipper.baseURL}/api/queue`)).json()).count)
    .toBe(1);

  release();

  // The baseline is collected after subscribing, so it carries the queued deploy.
  await expect(page.locator('[data-testid="pending-pill"]')).toBeVisible();
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
  // The idle text is hidden (only the anchor glyph shows); the state lives in
  // aria-label, present from initial render.
  await expect(indicator).toHaveAttribute('aria-label', 'idle');

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

// UD11 — Tap-tip opt-in on non-header controls. setupTapTips used to only
// understand two hardcoded cases (inside <header>, or the hooks badge);
// everything else with a title — the deploy row's container-logs icon here —
// sat silently on touch. It now gates on a `data-taptip` opt-in instead (an
// ancestor or the control itself), so this asserts the bubble on a leaf-level
// opt-in, that mouse/pen still gets no bubble, and that a control inside
// `.view-options` stays silent even though it sits under the opted-in
// <header> (that popover's rows already show a visible label).
test.describe('UD11: tap-tip opt-in on non-header controls', () => {
  test.use({ startOptions: { stacks: ['web'] } });

  test('a non-header glyph flashes on touch, stays silent on mouse, and auto-hides', async ({
    page,
    skipper,
  }) => {
    await page.clock.install(); // before navigation, so the bubble's own setTimeout is captured
    await page.goto(`${skipper.baseURL}/`);

    // The cross-view jump glyph is a non-header, always-visible row control that
    // opts into the tap-tip (the container-logs glyph now sits inside the ⋯ menu,
    // labelled, so it no longer carries a tap-tip — T3.13).
    const jumpBtn = page.locator('[data-testid="deploy-row"][data-stack="web"] [data-testid="jump-btn"]');
    const tip = page.locator('.tap-tip');
    const title = await jumpBtn.getAttribute('title');

    await jumpBtn.dispatchEvent('pointerdown', { pointerType: 'touch' });
    await expect(tip).toHaveClass(/\bshow\b/);
    await expect(tip).toHaveText(title!);

    await page.clock.fastForward('00:02'); // past the 1600ms auto-hide timer
    await expect(tip).not.toHaveClass(/\bshow\b/);

    // Mouse/pen keep the native tooltip only — no bubble.
    await jumpBtn.dispatchEvent('pointerdown', { pointerType: 'mouse' });
    await expect(tip).not.toHaveClass(/\bshow\b/);
  });

  test('a control inside .view-options stays silent even though <header> opts in', async ({
    page,
    skipper,
  }) => {
    await page.goto(`${skipper.baseURL}/`);

    // Open the popover so the excluded control is actually reachable (UD8).
    // time-mode, not deploy-search: the search row is desktop-hidden (UG3),
    // so it wouldn't be visible here regardless of the tap-tip exclusion.
    await page.locator('[data-testid="view-toggle"] button[data-view="deploys"]').click();
    const timeMode = page.locator('[data-testid="time-mode"]');
    await expect(timeMode).toBeVisible();

    await timeMode.dispatchEvent('pointerdown', { pointerType: 'touch' });
    await expect(page.locator('.tap-tip')).not.toHaveClass(/\bshow\b/);
  });
});
