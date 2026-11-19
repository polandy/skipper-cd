import { test, expect } from '../fixtures/test';
import type { Skipper } from '../fixtures/harness';
import type { Locator, Page } from '@playwright/test';

// Maske AF: status badges lead with an icon, and the two worst terminal states
// render as a solid alert chip rather than the smallest text in the column
// (T3.14 — inverted hierarchy). See docs/dev/e2e-tests.md §4.33.
//
// Drives the real self-heal loop (like Maske K) so three badges surface in one
// run — the startup `success`, a corrective `healed` (a first outage the
// redeploy fixes), and the `heal_exhausted` give-up (a second one it cannot) — then asserts each carries a `.badge-ico` glyph and that the worst
// state is an opaque (solid) fill, distinct from a dim badge. The colour read is
// off the settled computed style, not a timed effect. Behaviour-only.

const healthyApp = [{ Service: 'app', State: 'running', Health: 'healthy' }];
const unhealthyApp = [{ Service: 'app', State: 'running', Health: 'unhealthy' }];

const webRow = (page: Page, status: string): Locator =>
  page.locator(`[data-testid="deploy-row"][data-stack="web"][data-status="${status}"]`);

// bgAlpha reads a badge's settled background-color and returns its alpha channel
// (1 for an opaque `rgb(...)`, the parsed value for the `rgba(...)` / `rgb(r g b
// / a)` forms a dim `color-mix` serialises to). A deterministic DOM read.
async function bgAlpha(badge: Locator): Promise<number> {
  return badge.evaluate((el) => {
    const bg = getComputedStyle(el).backgroundColor;
    const nums = bg
      .replace(/[^0-9.,/ ]/g, '')
      .split(/[\s,/]+/)
      .filter((s) => s !== '');
    return nums.length >= 4 ? parseFloat(nums[3]) : 1;
  });
}

test.describe('UAF: status badges carry icons; worst states are solid', () => {
  test.use({
    startOptions: {
      stacks: ['web'],
      healthPoll: 1,
      selfHeal: true,
      selfHealMinUnhealthyPolls: 1,
      selfHealCooldownSeconds: 1,
      selfHealMaxAttempts: 1,
      initialHealth: { web: healthyApp },
    },
  });

  const successBadgeOf = (page: Page): Locator =>
    webRow(page, 'success').first().locator('[data-testid="status-badge"]');

  // An outage the one permitted redeploy cannot fix trips the breaker.
  async function exhaust(skipper: Skipper): Promise<void> {
    const ups = skipper.dockerUps('web');
    skipper.setStackHealth('web', unhealthyApp);
    await expect(() => expect(skipper.dockerUps('web')).toBeGreaterThan(ups)).toPass();
  }

  test('UAF1: the success badge carries a leading icon and keeps its text', async ({
    page,
    skipper,
  }) => {
    await page.goto(`${skipper.baseURL}/`);
    const badge = successBadgeOf(page);
    await expect(badge).toBeVisible();
    await expect(badge.locator('svg.badge-ico')).toHaveCount(1);
    await expect(badge).toHaveText('success');
  });

  // A budget of two keeps a poll that lands between the redeploy and the
  // recovery from exhausting the breaker before the heal is seen.
  test.describe('after a heal', () => {
    test.use({
      startOptions: {
        stacks: ['web'],
        healthPoll: 1,
        selfHeal: true,
        selfHealMinUnhealthyPolls: 1,
        selfHealCooldownSeconds: 1,
        selfHealMaxAttempts: 2,
        initialHealth: { web: healthyApp },
      },
    });

    test('UAF2: the healed badge also leads with an icon', async ({ page, skipper }) => {
      await page.goto(`${skipper.baseURL}/`);
      const ups = skipper.dockerUps('web');
      skipper.setStackHealth('web', unhealthyApp);
      await expect(() => expect(skipper.dockerUps('web')).toBeGreaterThan(ups)).toPass();
      skipper.setStackHealth('web', healthyApp);
      const healed = webRow(page, 'healed').first();
      await expect(healed).toBeVisible();
      await expect(healed.locator('[data-testid="status-badge"] svg.badge-ico')).toHaveCount(1);
    });
  });

  test('UAF3: heal_exhausted is a two-line chip with a warning icon', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);
    await exhaust(skipper);
    const exhausted = webRow(page, 'heal_exhausted').first();
    await expect(exhausted).toBeVisible();
    const badge = exhausted.locator('[data-testid="status-badge"]');
    await expect(badge.locator('svg.badge-ico')).toHaveCount(1);
    await expect(badge).toContainText('self-heal');
    await expect(badge).toContainText('failed');
    await expect(badge.locator('.badge-lbl span')).toHaveCount(2);
  });

  test('UAF4: the heal_exhausted fill is opaque while a dim badge is translucent', async ({
    page,
    skipper,
  }) => {
    await page.goto(`${skipper.baseURL}/`);
    const dim = successBadgeOf(page);
    await expect(dim).toBeVisible();
    await exhaust(skipper);
    const exhausted = webRow(page, 'heal_exhausted').first();
    await expect(exhausted).toBeVisible();
    expect(await bgAlpha(exhausted.locator('[data-testid="status-badge"]'))).toBeCloseTo(1, 2);
    expect(await bgAlpha(dim)).toBeLessThan(0.9);
  });
});
