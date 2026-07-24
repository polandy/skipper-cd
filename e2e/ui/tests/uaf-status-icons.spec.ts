import { test, expect } from '../fixtures/test';
import type { Locator, Page } from '@playwright/test';

// Maske AF: status badges lead with an icon, and the two worst terminal states
// render as a solid alert chip rather than the smallest text in the column
// (T3.14 — inverted hierarchy). See dev-docs/e2e-tests.md §4.33.
//
// Drives the real self-heal loop (like Maske K) so three badges surface in one
// run — the startup `success`, a corrective `healed`, and the `heal_exhausted`
// give-up — then asserts each carries a `.badge-ico` glyph and that the worst
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

  test('every badge leads with an icon and heal_exhausted is a solid chip', async ({
    page,
    skipper,
  }) => {
    await page.goto(`${skipper.baseURL}/`);

    // UAF1 — the startup `success` badge carries a leading icon; the svg adds no
    // text, so the label is unchanged.
    const success = webRow(page, 'success').first();
    await expect(success).toBeVisible();
    const successBadge = success.locator('[data-testid="status-badge"]');
    await expect(successBadge.locator('svg.badge-ico')).toHaveCount(1);
    await expect(successBadge).toHaveText('success');

    // The stack turns unhealthy → one corrective heal, then the breaker trips.
    skipper.setStackHealth('web', unhealthyApp);

    // UAF2 — the `healed` badge also leads with an icon.
    const healed = webRow(page, 'healed').first();
    await expect(healed).toBeVisible();
    await expect(healed.locator('[data-testid="status-badge"] svg.badge-ico')).toHaveCount(1);

    // UAF3 — the worst terminal state: a warning icon, both stacked label lines,
    // and a SOLID fill — the loudest chip, reversing the old 9px shrink (T3.14).
    const exhausted = webRow(page, 'heal_exhausted').first();
    await expect(exhausted).toBeVisible();
    const exhaustedBadge = exhausted.locator('[data-testid="status-badge"]');
    await expect(exhaustedBadge.locator('svg.badge-ico')).toHaveCount(1);
    await expect(exhaustedBadge).toContainText('self-heal');
    await expect(exhaustedBadge).toContainText('failed');
    await expect(exhaustedBadge.locator('.badge-lbl span')).toHaveCount(2);

    // UAF4 — the worst-state chip is opaque; a dim badge (success) is translucent.
    expect(await bgAlpha(exhaustedBadge)).toBeCloseTo(1, 2);
    expect(await bgAlpha(successBadge)).toBeLessThan(0.9);
  });
});
