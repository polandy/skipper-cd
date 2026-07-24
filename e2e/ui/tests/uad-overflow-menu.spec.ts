import { test, expect } from '../fixtures/test';
import { openRowMenu } from '../fixtures/menu';

// Maske AD: the deploy row's ⋯ overflow menu (T3.13). See dev-docs/e2e-tests.md §4.31.
//
// The newest row per stack collapses its secondary actions — deploy history,
// container logs and deploy hooks — behind one ⋯ button, so the resting row is
// identity + status + the (still-visible) jump action instead of a cluster of
// look-alike glyphs. Boots in discovery mode with a hooks-declaring stack so all
// three actions are present. Behaviour-only (no snapshot).

// web declares 2 pre + 1 post hook (harmless echoes) so its ⋯ menu carries the
// full set: history, container logs and deploy hooks.
const WEB_HOOKS = `stacks:
  web:
    hooks:
      pre_deploy:
        - "echo starting backup"
        - "echo dumping database"
      post_deploy:
        - "echo verifying deploy"
`;

test.use({ startOptions: { stacks: ['web'], discovery: { repoConfig: WEB_HOOKS } } });

const webRow = (page: import('@playwright/test').Page) =>
  page.locator('[data-testid="deploy-row"][data-stack="web"]');

// UAD1 — the secondary actions are collapsed behind ⋯; opening it reveals them
// as labelled rows, while the jump action stays on the row.
test('UAD1: ⋯ collapses history/logs/hooks and opens a labelled menu', async ({ page, skipper }) => {
  await page.goto(`${skipper.baseURL}/`);

  const row = webRow(page);
  const more = row.locator('[data-testid="more-btn"]');
  const pop = row.locator('[data-testid="more-pop"]');

  // The row rests calm: a ⋯ button, the jump action still directly on the row,
  // and none of the collapsed action glyphs loose in the stack cell.
  await expect(more).toBeVisible();
  await expect(row.locator('[data-testid="jump-btn"]')).toBeVisible();
  await expect(row.locator('.cell-stack > [data-testid="history-btn"]')).toHaveCount(0);
  await expect(row.locator('.cell-stack > [data-testid="clog-btn"]')).toHaveCount(0);
  await expect(row.locator('.cell-stack > [data-testid="hooks-badge"]')).toHaveCount(0);

  // Closed by default: the actions exist but are hidden until the menu opens.
  await expect(pop).toBeHidden();
  await expect(more).toHaveAttribute('aria-expanded', 'false');
  await expect(row.locator('[data-testid="history-btn"]')).toBeHidden();

  // Open: the three actions show as labelled rows inside the popover.
  await more.click();
  await expect(pop).toBeVisible();
  await expect(more).toHaveAttribute('aria-expanded', 'true');
  await expect(pop.locator('[data-testid="history-btn"]')).toBeVisible();
  await expect(pop.locator('[data-testid="clog-btn"]')).toBeVisible();
  await expect(pop.locator('[data-testid="hooks-badge"]')).toBeVisible();
  await expect(pop).toContainText('Deploy history');
  await expect(pop).toContainText('Container logs');
  await expect(pop).toContainText('Deploy hooks');
});

// UAD2 — picking an action runs it (opens its panel) and closes the menu.
test('UAD2: selecting Deploy history opens its panel and closes the menu', async ({ page, skipper }) => {
  await page.goto(`${skipper.baseURL}/`);
  const row = webRow(page);

  await openRowMenu(row);
  await row.locator('[data-testid="history-btn"]').click();

  await expect(page.locator('[data-testid="audit-panel"]')).toBeVisible();
  await expect(row).toHaveClass(/audit-open/);
  // The menu closes on selection.
  await expect(row.locator('[data-testid="more-pop"]')).toBeHidden();
  await expect(row.locator('[data-testid="more-btn"]')).toHaveAttribute('aria-expanded', 'false');
});

// UAD3 — the same for the container-logs action, proving the relocated clog
// button keeps its own handler inside the menu.
test('UAD3: selecting Container logs opens the log panel and closes the menu', async ({ page, skipper }) => {
  await page.goto(`${skipper.baseURL}/`);
  const row = webRow(page);

  await openRowMenu(row);
  await row.locator('[data-testid="clog-btn"]').click();

  await expect(page.locator('[data-testid="clog-panel"]')).toBeVisible();
  await expect(row.locator('[data-testid="more-pop"]')).toBeHidden();
});

// UAD4 — the menu dismisses on an outside click and on Escape, like the
// app-link popover.
test('UAD4: the menu closes on outside click and on Escape', async ({ page, skipper }) => {
  await page.goto(`${skipper.baseURL}/`);
  const row = webRow(page);
  const more = row.locator('[data-testid="more-btn"]');
  const pop = row.locator('[data-testid="more-pop"]');

  // Outside click.
  await more.click();
  await expect(pop).toBeVisible();
  await page.locator('#deploy-table').click({ position: { x: 5, y: 5 } });
  await expect(pop).toBeHidden();
  await expect(more).toHaveAttribute('aria-expanded', 'false');

  // Escape.
  await more.click();
  await expect(pop).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(pop).toBeHidden();
  await expect(more).toHaveAttribute('aria-expanded', 'false');
});

// UAD5 — on a narrow portrait viewport the popover stays fully within the
// screen (it flips right-aligned near the edge) instead of spilling past it —
// the density fix must not reintroduce an overlap in portrait.
test.describe('UAD5: portrait — popover never spills off-screen', () => {
  test.use({ viewport: { width: 400, height: 860 } });

  test('the open menu sits within the viewport bounds', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);
    const row = webRow(page);

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
