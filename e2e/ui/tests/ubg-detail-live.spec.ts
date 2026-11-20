import { test, expect } from '../fixtures/test';
import type { Page } from '@playwright/test';
import { openStackDetail } from '../fixtures/stack-detail';

// Maske BG: an open detail follows the live state. See docs/dev/e2e-tests.md
// §4.60 and internal/ui/UI_SPEC.md "Expandable panels".
//
// The health poller runs every second over the stub's scripted container state
// (skipper.setStackHealth). A detail opened before a health change shows the
// change in place, staying open on the same panel element (UBG1, UBG2), and a
// re-render keeps the focus on the control that had it (UBG3) and an open
// container log open (UBG4). The roster row
// of a second stack proves a snapshot landed before the panel opened.
// Behaviour-only (no snapshot).

const healthy = [{ Service: 'app', State: 'running', Health: 'healthy' }];
const degraded = [
  { Service: 'app', State: 'running', Health: 'healthy' },
  { Service: 'db', State: 'running', Health: 'unhealthy' },
];

const panel = (page: Page, stack: string) =>
  page.locator(`[data-testid="detail-panel"][data-stack="${stack}"]`);
const rosterRow = (page: Page, stack: string) =>
  page.locator(`[data-testid="roster-row"][data-stack="${stack}"]`);

// markPanel tags the open panel element, so a later check can tell an
// in-place re-render from a closed and reopened panel.
async function markPanel(page: Page, stack: string) {
  await panel(page, stack).evaluate((p) => p.setAttribute('data-e2e-mark', '1'));
}

test.describe('Maske BG: an open detail follows the live state', () => {
  test.use({
    startOptions: {
      stacks: ['web', 'api'],
      healthPoll: 1,
      initialHealth: { web: healthy, api: healthy },
    },
  });

  // UBG1 — the Stacks detail's Containers tab re-renders on a health change.
  test('UBG1: the open Containers tab shows a health change', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);
    await openStackDetail(page, 'web', 'containers');
    const hp = panel(page, 'web').locator('[data-testid="health-panel"]');
    await expect(hp).toHaveAttribute('data-health', 'healthy');
    await expect(hp.locator('[data-testid="health-service"]')).toHaveCount(1);
    await markPanel(page, 'web');

    skipper.setStackHealth('web', degraded);
    await expect(hp).toHaveAttribute('data-health', 'unhealthy');
    await expect(hp.locator('[data-testid="health-service"]')).toHaveCount(2);
    await expect(panel(page, 'web')).toHaveAttribute('data-e2e-mark', '1');
    await expect(panel(page, 'web')).toHaveAttribute('data-tab', 'containers');
  });

  // UBG3 — a re-render keeps the focus on the tab that had it.
  test('UBG3: a live re-render keeps the focused tab focused', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);
    await openStackDetail(page, 'web', 'containers');
    const hp = panel(page, 'web').locator('[data-testid="health-panel"]');
    await expect(hp).toHaveAttribute('data-health', 'healthy');
    const tab = panel(page, 'web').locator('[data-testid="detail-tab"][data-tab="containers"]');
    await tab.focus();

    skipper.setStackHealth('web', degraded);
    await expect(hp).toHaveAttribute('data-health', 'unhealthy');
    await expect(tab).toBeFocused();
  });

  // UBG4 — a re-render keeps an open container log open under its new button.
  test('UBG4: a live re-render keeps the open container log', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);
    await openStackDetail(page, 'web', 'containers');
    const hp = panel(page, 'web').locator('[data-testid="health-panel"]');
    await expect(hp).toHaveAttribute('data-health', 'healthy');
    await hp.locator('[data-testid="health-service"]').first().locator('[data-testid="clog-btn"]').click();
    const log = page.locator('[data-testid="clog-panel"]');
    await expect(log).toBeVisible();

    skipper.setStackHealth('web', degraded);
    await expect(hp).toHaveAttribute('data-health', 'unhealthy');
    await expect(log).toBeVisible();
    await expect(panel(page, 'web').locator('[data-testid="clog-panel"]')).toHaveCount(1);
  });
});

// UBG2 seeds web with no containers before boot, so every poll reports none
// until the test scripts one.
test.describe('Maske BG: the deploy detail follows the live state', () => {
  test.use({
    startOptions: {
      stacks: ['web', 'api'],
      healthPoll: 1,
      initialHealth: { web: [], api: healthy },
    },
  });

  // UBG2 — the deploy detail gains its Logs action once the stack reports containers.
  test('UBG2: the open deploy detail picks up a health change', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);
    await expect(rosterRow(page, 'api')).toHaveAttribute('data-health', 'healthy');
    await page.locator('[data-testid="deploy-row"][data-stack="web"] .stack-name').first().click();
    const logs = panel(page, 'web').locator('.dp-head [data-testid="clog-btn"]');
    await expect(panel(page, 'web')).toBeVisible();
    await expect(logs).toHaveCount(0);
    await markPanel(page, 'web');

    skipper.setStackHealth('web', healthy);
    await expect(logs).toBeVisible();
    await expect(panel(page, 'web')).toHaveAttribute('data-e2e-mark', '1');
  });
});
