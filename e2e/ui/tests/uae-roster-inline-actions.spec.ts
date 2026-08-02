import { test, expect } from '../fixtures/test';
import type { Page } from '@playwright/test';

// Maske AE: the Stacks/roster row's inline secondary actions. See
// dev-docs/e2e-tests.md §4.32.
//
// The roster row surfaces its secondary actions inline in the stack cell: the
// container-logs icon always, the deploy-hooks badge only when the stack
// declares hooks. They sit beside the jump + app-link icons. Earlier these were
// folded behind a ⋯ overflow menu (as the deploy row still is, §4.31), but on the
// roster the row-body click already opens the health + history panel, so the menu
// usually wrapped a single action (logs) — an extra click for no density gain.
// There is no ⋯ on the roster. Boots in discovery mode with one hooks-declaring
// stack (web) and one without (api). Behaviour-only (no snapshot).

// web declares hooks so it shows the inline hooks badge; api declares none, so
// it shows the logs icon alone.
const HOOKS_CONFIG = `stacks:
  web:
    hooks:
      pre_deploy:
        - "echo starting backup"
      post_deploy:
        - "echo verifying deploy"
`;

test.use({ startOptions: { stacks: ['web', 'api'], discovery: { repoConfig: HOOKS_CONFIG } } });

const stacksBtn = (page: Page) => page.locator('[data-testid="view-toggle"] button[data-view="stacks"]');
const rosterRow = (page: Page, stack: string) =>
  page.locator(`[data-testid="roster-row"][data-stack="${stack}"]`);

async function openStacks(page: Page, skipper: { baseURL: string }) {
  await page.goto(`${skipper.baseURL}/`);
  await stacksBtn(page).click();
  await expect(page.locator('[data-testid="stacks-view"]')).toBeVisible();
}

// UAE1 — the secondary actions sit inline in the stack cell, not behind a ⋯. The
// hooks badge shows only for a hooks-declaring stack; there is no more-btn.
test('UAE1: logs (+ hooks) render inline in the row, with no ⋯ menu', async ({ page, skipper }) => {
  await openStacks(page, skipper);

  // Jump, logs and hooks all sit in the stack cell's own action cluster — one
  // box, so a narrow row wraps them together rather than between two icons.
  const actions = rosterRow(page, 'web').locator('.roster-stack > .row-actions');
  await expect(actions.locator('> [data-testid="jump-btn"]')).toBeVisible();
  await expect(actions.locator('> [data-testid="clog-btn"]')).toBeVisible();
  await expect(actions.locator('> [data-testid="hooks-badge"]')).toBeVisible();
  await expect(actions.locator('> [data-testid="hooks-badge"]')).toContainText('1+1'); // 1 pre, 1 post

  // No overflow menu anywhere on the roster.
  await expect(page.locator('[data-testid="roster-list"] [data-testid="more-btn"]')).toHaveCount(0);

  // A hooks-less stack shows the logs icon but no hooks badge.
  const apiActions = rosterRow(page, 'api').locator('.roster-stack > .row-actions');
  await expect(apiActions.locator('> [data-testid="clog-btn"]')).toBeVisible();
  await expect(apiActions.locator('> [data-testid="hooks-badge"]')).toHaveCount(0);
});

// UAE2 — the inline logs icon opens the live log panel directly (one click).
test('UAE2: the inline logs icon opens the log panel', async ({ page, skipper }) => {
  await openStacks(page, skipper);
  const web = rosterRow(page, 'web');

  await web.locator('.roster-stack .row-actions > [data-testid="clog-btn"]').click();

  const panel = page.locator('[data-testid="clog-panel"]');
  await expect(panel).toBeVisible();
  await expect(panel).toContainText('web');

  // A second click on the same icon toggles it closed.
  await web.locator('.roster-stack .row-actions > [data-testid="clog-btn"]').click();
  await expect(panel).toHaveCount(0);
});

// UAE3 — the inline hooks badge opens the bound hooks panel (the configured
// commands) and toggles closed on a second click.
test('UAE3: the inline hooks badge opens the hooks panel', async ({ page, skipper }) => {
  await openStacks(page, skipper);
  const web = rosterRow(page, 'web');
  const badge = web.locator('.roster-stack .row-actions > [data-testid="hooks-badge"]');

  await badge.click();
  const panel = page.locator('[data-testid="hooks-panel"]');
  await expect(panel).toBeVisible();
  await expect(panel).toContainText('starting backup');
  await expect(web).toHaveClass(/hooks-open/);

  // Toggle closed (exclusive with the row-body history panel).
  await badge.click();
  await expect(panel).toBeHidden();
  await expect(web).not.toHaveClass(/hooks-open/);
});

// UAE4 — on a narrow portrait viewport the inline cluster stays within the row
// and the logs icon still opens its panel below the row (no off-screen spill).
test.describe('UAE4: portrait — inline actions stay usable', () => {
  test.use({ viewport: { width: 400, height: 860 } });

  test('the inline actions fit the row and the log opens below it', async ({ page, skipper }) => {
    await openStacks(page, skipper);
    const clog = rosterRow(page, 'web').locator('.roster-stack .row-actions > [data-testid="clog-btn"]');

    await expect(clog).toBeVisible();
    const box = await clog.boundingBox();
    const vp = page.viewportSize();
    expect(box).not.toBeNull();
    expect(vp).not.toBeNull();
    expect(box!.x).toBeGreaterThanOrEqual(0);
    expect(box!.x + box!.width).toBeLessThanOrEqual(vp!.width);

    await clog.click();
    await expect(page.locator('[data-testid="clog-panel"]')).toBeVisible();
  });
});

// UAE5 — a tablet-width row whose name is long enough to push the affordances
// off the first line wraps them as ONE cluster: the icons stayed together, and
// the row does not split a jump icon from the log icon beside it.
test.describe('UAE5: tablet — a wrapping row keeps its glyphs on one line', () => {
  const LONG = 'docker-registry-proxy-service';
  test.use({
    startOptions: { stacks: [LONG] },
    viewport: { width: 744, height: 1133 }, // iPad mini, portrait
  });

  test('the action cluster wraps whole, not glyph by glyph', async ({ page, skipper }) => {
    await openStacks(page, skipper);

    const boxes = await rosterRow(page, LONG).evaluate((row) => {
      // Vertical centre, not top: the glyphs are centre-aligned and differ in
      // height, so equal tops would be the wrong question.
      const mid = (el: Element) => {
        const b = el.getBoundingClientRect();
        return Math.round(b.top + b.height / 2);
      };
      const cluster = row.querySelector('.roster-stack > .row-actions')!;
      return {
        name: mid(row.querySelector('.roster-name')!),
        cluster: mid(cluster),
        glyphs: [...cluster.children].map(mid),
      };
    });

    // The name is long enough that the cluster really did move down a line —
    // without this the same-line check below would pass vacuously.
    expect(boxes.cluster).toBeGreaterThan(boxes.name);
    // …and every glyph moved with it.
    expect(boxes.glyphs.length).toBeGreaterThan(1);
    for (const y of boxes.glyphs) expect(Math.abs(y - boxes.glyphs[0])).toBeLessThanOrEqual(2);
  });
});
