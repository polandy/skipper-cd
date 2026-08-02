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

// UAE5 — a tablet-width row carrying the full glyph set (jump, app link, logs,
// hooks) runs out of first-line width and wraps them as ONE cluster: the icons
// stay together, and the version keeps the name's line rather than sliding to
// the middle of the now two-line row.
test.describe('UAE5: tablet — a wrapping row keeps its glyphs on one line', () => {
  // 15 chars: wide enough that the four glyphs cannot follow it on the first
  // line, narrow enough that the name itself still shares that line with the
  // host chip (a name long enough to take a line of its own is a different
  // shape, and would make the alignment check below meaningless).
  const STACK = 'changedetection';
  test.use({
    startOptions: {
      stacks: [STACK],
      discovery: {
        repoConfig: `stacks:\n  ${STACK}:\n    hooks:\n      pre_deploy:\n        - "echo backup"\n`,
      },
      healthPoll: 1, // drives both the version chip and app-link detection
      appLinks: { [STACK]: ['watch.e2e.test'] },
      initialHealth: {
        [STACK]: [{ Service: 'app', Image: 'nginx:1.25', State: 'running', Health: 'healthy' }],
      },
    },
    viewport: { width: 744, height: 1133 }, // iPad mini, portrait
  });

  test('the cluster wraps whole, and the version stays on the name line', async ({
    page,
    skipper,
  }) => {
    await openStacks(page, skipper);
    const row = rosterRow(page, STACK);
    // Both arrive on a poll; measuring before they land would read a half-built row.
    await expect(row.locator('[data-testid="app-link-btn"]')).toBeVisible();
    await expect(row.locator('[data-testid="roster-version"] > *')).toBeVisible();

    const boxes = await row.evaluate((r) => {
      // Vertical centre, not top: these elements are centre-aligned within
      // their line and differ in height, so equal tops would be the wrong
      // question.
      const mid = (el: Element) => {
        const b = el.getBoundingClientRect();
        return Math.round(b.top + b.height / 2);
      };
      const cluster = r.querySelector('.roster-stack > .row-actions')!;
      return {
        chip: mid(r.querySelector('.roster-ident > :first-child')!),
        name: mid(r.querySelector('.roster-name')!),
        cluster: mid(cluster),
        glyphs: [...cluster.children].map(mid),
        version: mid(r.querySelector('[data-testid="roster-version"] > *')!),
      };
    });

    // The fixture holds: the name shares the first line with the host chip...
    expect(Math.abs(boxes.name - boxes.chip)).toBeLessThanOrEqual(3);
    // ...and the glyphs really did move to a second line — without this the
    // same-line check below would pass vacuously.
    expect(boxes.cluster).toBeGreaterThan(boxes.name);
    // Every glyph moved together.
    expect(boxes.glyphs.length).toBeGreaterThan(1);
    for (const y of boxes.glyphs) expect(Math.abs(y - boxes.glyphs[0])).toBeLessThanOrEqual(2);
    // The taller row must not drag the version down with it: it belongs to the
    // name and stays on the name's line (top-aligned cells).
    expect(Math.abs(boxes.version - boxes.name)).toBeLessThanOrEqual(3);
  });
});

// UAE6 — the app-link icon is rebuilt on every health poll (updateAppLinks),
// long after the row rendered. It must land back *inside* the cluster, ahead of
// the logs icon: appended to the cell it would sit outside and wrap alone again.
test.describe('UAE6: a re-polled app link stays in the cluster', () => {
  test.use({
    startOptions: {
      stacks: ['web', 'api'],
      healthPoll: 1, // app-link detection rides the health cadence
      appLinks: { web: ['web.e2e.test'] },
    },
  });

  test('the app-link icon sits inside the cluster, before the logs icon', async ({
    page,
    skipper,
  }) => {
    await openStacks(page, skipper);
    const cell = rosterRow(page, 'web').locator('.roster-stack');

    // The link needs a detection poll, so this also waits out at least one
    // updateAppLinks pass — the path that used to append it to the cell.
    await expect(cell.locator('[data-testid="app-link-btn"]')).toBeVisible();

    const shape = await cell.evaluate((c) => ({
      strays: [...c.children].filter((e) =>
        e.matches('.link-wrap, .jump-btn, .clog-btn, .hooks-badge'),
      ).length,
      cluster: [...c.querySelector('.row-actions')!.children].map((e) => e.className.split(' ')[0]),
    }));
    expect(shape.strays).toBe(0); // no glyph outside the cluster
    expect(shape.cluster).toEqual(['jump-btn', 'link-wrap', 'clog-btn']);
  });
});

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
