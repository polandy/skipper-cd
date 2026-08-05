import { test, expect } from '../fixtures/test';
import type { Page } from '@playwright/test';

// Maske AR: the Logs view fits the display. See dev-docs/e2e-tests.md §4.45.
//
// The 2026-08-05 report from a tablet: the log panel hung off the right edge of
// the screen. The cause was layout, not content — `main`'s auto side margins
// suppress the flex stretch in the Logs view's column, leaving it shrink-to-fit,
// i.e. at least its min-content width; one pre-formatted diff line inside the
// pane is wider than a tablet, so the whole column grew past the viewport and
// the page clipped it. The mask drives a deploy whose diff carries a
// deliberately long image reference and asserts the column stays inside the
// viewport on a tablet (UAR1) and a phone (UAR2) — the long line has to scroll
// inside the pane instead. It also pins the chrome the same report reshaped:
// the panel has no title bar and its filter row carries live + the tools
// (UAR3), and fullscreen never outlives the view it belongs to (UAR4).
// Behaviour-only, no snapshot.

// Long enough that its diff line cannot fit any of the viewports below, so the
// pane is the only thing that may scroll sideways.
const LONG_IMAGE =
  'registry.example.com/platform/team/service-with-a-very-long-name:1.4.2-alpine-hardened-build';

const logView = (page: Page) => page.locator('#log-view');
const quickBar = (page: Page) => page.locator('[data-testid="log-quick-bar"]');

async function openLogsView(page: Page): Promise<void> {
  const logsBtn = page.locator('[data-testid="view-toggle"] button[data-view="logs"]');
  if (!(await logsBtn.evaluate((b) => b.classList.contains('active')))) {
    await logsBtn.click();
  }
}

// Pushes a change whose diff is one very long line, then opens the Logs view
// with that diff rendered — the exact content the layout used to break on.
async function deployWideDiff(
  page: Page,
  skipper: import('../fixtures/harness').Skipper,
): Promise<void> {
  await page.goto(`${skipper.baseURL}/`);
  skipper.setStackServices('web', { app: LONG_IMAGE });
  expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);
  await openLogsView(page);
  await expect(page.locator('[data-testid="log-diff"]').first()).toBeVisible();
}

// The page column must never reach past the viewport. The diff block inside it
// may — that is what its own horizontal scroll is for.
async function expectColumnFitsViewport(page: Page): Promise<void> {
  const fit = await page.evaluate(() => {
    const main = document.querySelector('main')!;
    const view = document.getElementById('log-view')!;
    return {
      inner: window.innerWidth,
      mainScrollWidth: main.scrollWidth,
      mainRight: Math.round(main.getBoundingClientRect().right),
      panelRight: Math.round(view.getBoundingClientRect().right),
      diffScrolls: (() => {
        const d = document.querySelector<HTMLElement>('#log-pane .log-diff')!;
        return d.scrollWidth > d.clientWidth;
      })(),
    };
  });
  expect(fit.mainScrollWidth).toBeLessThanOrEqual(fit.inner);
  expect(fit.mainRight).toBeLessThanOrEqual(fit.inner);
  expect(fit.panelRight).toBeLessThanOrEqual(fit.inner);
  // Positive signal that the wide content really is on screen: it had to go
  // somewhere, and the diff block's own horizontal scroll is where it belongs.
  expect(fit.diffScrolls).toBe(true);
}

test.describe('Maske AR: the Logs view fits the display', () => {
  test.use({ startOptions: { stacks: ['web'] } });

  // UAR1 — a tablet: the reported viewport (iPad mini, portrait).
  test.describe('UAR1: tablet', () => {
    test.use({ viewport: { width: 744, height: 1133 } }); // iPad mini, portrait

    test('a wide diff scrolls inside the pane, not off the screen', async ({ page, skipper }) => {
      await deployWideDiff(page, skipper);
      await expectColumnFitsViewport(page);
    });
  });

  // UAR2 — a phone: the same guarantee where the chrome row wraps. Search moves
  // with it: the header magnifier is a desktop/tablet affordance, and the Logs
  // view has no view-options popover to carry it, so the in-panel tool is the
  // one entry point here and must be present.
  test.describe('UAR2: phone', () => {
    test.use({ viewport: { width: 390, height: 844 } });

    test('a wide diff scrolls inside the pane, not off the screen', async ({ page, skipper }) => {
      await deployWideDiff(page, skipper);
      await expectColumnFitsViewport(page);

      await expect(page.locator('[data-testid="stack-search-btn"]')).toBeHidden();
      await expect(quickBar(page).locator('[data-testid="log-search"]')).toBeVisible();
    });
  });

  // UAR3 — the panel's chrome is one row. The title bar is gone (the view
  // switch already names the view), so the filter row carries the live pill and
  // the pane tools; a viewer on a small screen loses a row of chrome, not a
  // control. Search is the exception: above the mobile breakpoint the header
  // magnifier owns it on every view, so the row shows no second magnifier.
  test.describe('UAR3: one chrome row', () => {
    test.use({ viewport: { width: 744, height: 1133 } });

    test('live and the tools live in the filter row and there is no title bar', async ({
      page,
      skipper,
    }) => {
      await page.goto(`${skipper.baseURL}/`);
      await openLogsView(page);

      await expect(logView(page).locator('.clog-head')).toHaveCount(0);
      for (const id of ['logs-live', 'log-wrap', 'follow-logs', 'log-fs']) {
        await expect(quickBar(page).locator(`[data-testid="${id}"]`)).toBeVisible();
      }
      // Search is the header magnifier here (Maske AB), so the row carries no
      // second magnifier of its own at this width.
      await expect(page.locator('[data-testid="stack-search-btn"]')).toBeVisible();
      await expect(quickBar(page).locator('[data-testid="log-search"]')).toBeHidden();

      // The controls still work from their new home: pausing reports paused.
      await page.locator('[data-testid="logs-live"]').click();
      await expect(page.locator('[data-testid="logs-live"]')).toHaveClass(/paused/);
      await expect(page.locator('[data-testid="logs-stat"]')).toHaveClass(/paused/);
    });
  });

  // UAR4 — fullscreen is a viewport-filling overlay, so it must not outlive the
  // view it belongs to: switching to Deploys used to leave it covering the page.
  test.describe('UAR4: fullscreen ends with the view', () => {
    test('switching views drops fullscreen and reveals the new view', async ({ page, skipper }) => {
      await page.goto(`${skipper.baseURL}/`);
      await openLogsView(page);

      await page.locator('[data-testid="log-fs"]').click();
      await expect(logView(page)).toHaveClass(/clog-fullscreen/);

      await page.locator('[data-testid="view-toggle"] button[data-view="deploys"]').click();
      await expect(logView(page)).toBeHidden();
      await expect(logView(page)).not.toHaveClass(/clog-fullscreen/);
      await expect(page.locator('#deploy-table')).toBeVisible();

      // Coming back shows an ordinary, windowed panel — fullscreen was exited,
      // not merely hidden.
      await openLogsView(page);
      await expect(logView(page)).toBeVisible();
      await expect(logView(page)).not.toHaveClass(/clog-fullscreen/);
      await expect(page.locator('[data-testid="log-fs"]')).not.toHaveClass(/\bon\b/);
    });
  });
});
