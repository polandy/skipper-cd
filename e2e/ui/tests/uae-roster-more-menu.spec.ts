import { test, expect } from '../fixtures/test';
import { openRowMenu } from '../fixtures/menu';
import type { Page } from '@playwright/test';

// Maske AE: the Stacks/roster row's ⋯ overflow menu (T3.13b). See
// dev-docs/e2e-tests.md §4.32.
//
// The roster row carried the same look-alike glyph cluster the Deploys row shed
// in T3.13 (#197). This folds its secondary actions — container logs and deploy
// hooks — behind the same ⋯ button, while jump + app-link stay inline as primary
// navigation. The menu reuses the shared ⋯ behaviour (one-open-at-a-time,
// outside-click / Escape dismiss) and the relocated buttons keep their own
// handlers, so opening the log / hooks panels is unchanged. Unlike Deploys the
// menu has no "Deploy history" item — on the roster the row-body click already
// opens the health + history panel. Boots in discovery mode with one hooks-
// declaring stack (web) and one without (api), so both the full and the single-
// item menu are covered. Behaviour-only (no snapshot).

// web declares hooks so its ⋯ carries both actions; api declares none, so its
// ⋯ carries container logs alone.
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

// UAE1 — the secondary actions are collapsed behind ⋯; opening it reveals them
// as labelled rows, while jump stays directly on the row. A hooks-less stack
// carries the same ⋯ with just the container-logs action.
test('UAE1: ⋯ collapses logs/hooks and opens a labelled menu; jump stays on the row', async ({ page, skipper }) => {
  await openStacks(page, skipper);

  const row = rosterRow(page, 'web');
  const more = row.locator('[data-testid="more-btn"]');
  const pop = row.locator('[data-testid="more-pop"]');

  // The row rests calm: a ⋯ button, the jump action still directly on the row,
  // and neither collapsed glyph loose in the stack cell.
  await expect(more).toBeVisible();
  await expect(row.locator('[data-testid="jump-btn"]')).toBeVisible();
  await expect(row.locator('.roster-stack > [data-testid="clog-btn"]')).toHaveCount(0);
  await expect(row.locator('.roster-stack > [data-testid="hooks-badge"]')).toHaveCount(0);

  // Closed by default: the actions exist but are hidden until the menu opens.
  await expect(pop).toBeHidden();
  await expect(more).toHaveAttribute('aria-expanded', 'false');

  // Open: the two actions show as labelled rows inside the popover. No "Deploy
  // history" item — the row-body click owns that on the roster.
  await more.click();
  await expect(pop).toBeVisible();
  await expect(more).toHaveAttribute('aria-expanded', 'true');
  await expect(pop.locator('[data-testid="clog-btn"]')).toBeVisible();
  await expect(pop.locator('[data-testid="hooks-badge"]')).toBeVisible();
  await expect(pop).toContainText('Container logs');
  await expect(pop).toContainText('Deploy hooks');
  await expect(pop).not.toContainText('Deploy history');

  // A stack without hooks carries the same ⋯, holding container logs alone.
  const apiRow = rosterRow(page, 'api');
  await apiRow.locator('[data-testid="more-btn"]').click();
  const apiPop = apiRow.locator('[data-testid="more-pop"]');
  await expect(apiPop.locator('[data-testid="clog-btn"]')).toBeVisible();
  await expect(apiPop.locator('[data-testid="hooks-badge"]')).toHaveCount(0);
});

// UAE2 — picking Container logs opens the live log panel and closes the menu,
// proving the relocated clog button keeps its own handler inside the menu.
test('UAE2: selecting Container logs opens the log panel and closes the menu', async ({ page, skipper }) => {
  await openStacks(page, skipper);
  const row = rosterRow(page, 'web');

  await openRowMenu(row);
  await row.locator('[data-testid="clog-btn"]').click();

  await expect(page.locator('[data-testid="clog-panel"]')).toBeVisible();
  await expect(row.locator('[data-testid="more-pop"]')).toBeHidden();
  await expect(row.locator('[data-testid="more-btn"]')).toHaveAttribute('aria-expanded', 'false');
});

// UAE3 — picking Deploy hooks opens the bound hooks panel (the configured
// commands) and closes the menu — the roster badge is only actionable from
// inside the ⋯ menu, and here it opens the same panel the Deploys badge does.
test('UAE3: selecting Deploy hooks opens the hooks panel and closes the menu', async ({ page, skipper }) => {
  await openStacks(page, skipper);
  const row = rosterRow(page, 'web');

  await openRowMenu(row);
  await row.locator('[data-testid="hooks-badge"]').click();

  const panel = page.locator('[data-testid="hooks-panel"]');
  await expect(panel).toBeVisible();
  await expect(panel).toContainText('starting backup');
  await expect(row).toHaveClass(/hooks-open/);
  await expect(row.locator('[data-testid="more-pop"]')).toBeHidden();

  // The panel is exclusive with the row-body history panel: a second selection
  // of the same action toggles it closed.
  await openRowMenu(row);
  await row.locator('[data-testid="hooks-badge"]').click();
  await expect(panel).toBeHidden();
  await expect(row).not.toHaveClass(/hooks-open/);
});

// UAE4 — the menu dismisses on an outside click and on Escape, like the Deploys
// menu and the app-link popover.
test('UAE4: the menu closes on outside click and on Escape', async ({ page, skipper }) => {
  await openStacks(page, skipper);
  const row = rosterRow(page, 'web');
  const more = row.locator('[data-testid="more-btn"]');
  const pop = row.locator('[data-testid="more-pop"]');

  // Outside click.
  await more.click();
  await expect(pop).toBeVisible();
  await page.locator('.roster-list-header').click({ position: { x: 5, y: 5 } });
  await expect(pop).toBeHidden();
  await expect(more).toHaveAttribute('aria-expanded', 'false');

  // Escape.
  await more.click();
  await expect(pop).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(pop).toBeHidden();
  await expect(more).toHaveAttribute('aria-expanded', 'false');
});

// UAE5 — on a narrow portrait viewport the popover stays fully within the screen
// (it flips right-aligned near the edge) instead of spilling past it — the
// density fix must not reintroduce an overlap in portrait.
test.describe('UAE5: portrait — popover never spills off-screen', () => {
  test.use({ viewport: { width: 400, height: 860 } });

  test('the open menu sits within the viewport bounds', async ({ page, skipper }) => {
    await openStacks(page, skipper);
    const row = rosterRow(page, 'web');

    await row.locator('[data-testid="more-btn"]').click();
    const pop = row.locator('[data-testid="more-pop"]');
    await expect(pop).toBeVisible();

    const box = await pop.boundingBox();
    const vp = page.viewportSize();
    expect(box).not.toBeNull();
    expect(vp).not.toBeNull();
    expect(box!.x).toBeGreaterThanOrEqual(0);
    expect(box!.x + box!.width).toBeLessThanOrEqual(vp!.width);
  });
});
