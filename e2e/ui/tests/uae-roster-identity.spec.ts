import { test, expect } from '../fixtures/test';
import type { Page } from '@playwright/test';

// Maske AE: the Stacks row's identity cell. See docs/dev/e2e-tests.md §4.32.
//
// UAE1–UAE6 covered the row's inline action cluster, which moved into the head
// of the stack's detail panel (Maske BC, §4.56). What remains is the identity:
// a long name ellipsises on its line rather than taking a line of its own.
// Behaviour-only (no snapshot).

const stacksBtn = (page: Page) => page.locator('[data-testid="view-toggle"] button[data-view="stacks"]');
const rosterRow = (page: Page, stack: string) =>
  page.locator(`[data-testid="roster-row"][data-stack="${stack}"]`);

async function openStacks(page: Page, skipper: { baseURL: string }) {
  await page.goto(`${skipper.baseURL}/`);
  await stacksBtn(page).click();
  await expect(page.locator('[data-testid="stacks-view"]')).toBeVisible();
}

// UAE7 — the identity (host chip + icon + name) is one non-wrapping box, so a
// name too long for the column ellipsises on the first line instead of taking a
// line of its own below the chip — which would leave the version, aligned to the
// row's first line, above the name it describes.
test.describe('UAE7: tablet — an over-long name ellipsises instead of taking a line', () => {
  const LONG = 'docker-registry-proxy-service';
  test.use({
    startOptions: {
      stacks: [LONG],
      healthPoll: 1,
      initialHealth: {
        [LONG]: [{ Service: 'app', Image: 'nginx:1.25', State: 'running', Health: 'healthy' }],
      },
    },
    viewport: { width: 744, height: 1133 }, // iPad mini, portrait
  });

  test('the name clips on the first line and keeps the version beside it', async ({
    page,
    skipper,
  }) => {
    await openStacks(page, skipper);
    const row = rosterRow(page, LONG);
    await expect(row.locator('[data-testid="roster-version"] > *')).toBeVisible();

    const boxes = await row.evaluate((r) => {
      const mid = (el: Element) => {
        const b = el.getBoundingClientRect();
        return Math.round(b.top + b.height / 2);
      };
      const name = r.querySelector('.roster-name') as HTMLElement;
      return {
        chip: mid(r.querySelector('.roster-ident > :first-child')!),
        name: mid(name),
        version: mid(r.querySelector('[data-testid="roster-version"] > *')!),
        clipped: name.scrollWidth > name.clientWidth,
        title: name.title,
      };
    });

    // Clipped, not moved: still on the chip's line, with the version beside it.
    expect(boxes.clipped).toBe(true);
    expect(Math.abs(boxes.name - boxes.chip)).toBeLessThanOrEqual(3);
    expect(Math.abs(boxes.version - boxes.name)).toBeLessThanOrEqual(3);
    // The clipped text stays reachable.
    expect(boxes.title).toBe(LONG);
  });
});
