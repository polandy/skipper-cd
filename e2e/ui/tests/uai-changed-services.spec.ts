import { test, expect } from '../fixtures/test';
import type { Page, Locator } from '@playwright/test';

// Maske AI: per-service image delta on a deploy row. See dev-docs/e2e-tests.md
// §4.36.
//
// A deploy carries `image_changes` (service, old, new) on its SSE payload; the
// row surfaces which service updated — and to what — inline after the stack name
// (its own line on a phone), without opening the diff. A webhook that rewrites a
// stack's compose commits against the startup commit, so the deploy runs for
// real (stub docker) and the delta renders from the actual image_changes.

test.use({ startOptions: { stacks: ['web', 'api', 'worker', 'database'] } });

// The newest (topmost) settled row for a stack — rows are prepended, so the
// just-deployed row is first.
const successRow = (page: Page, stack: string): Locator =>
  page.locator(`[data-testid="deploy-row"][data-stack="${stack}"][data-status="success"]`).first();

const delta = (page: Page, stack: string): Locator =>
  successRow(page, stack).locator('[data-testid="svc-delta"]');

// UI1 — a tag bump shows the changed service with its old→new tags (old struck
// through, new in the add colour). The default compose service is `app`, so the
// service is named (it differs from the stack).
test.describe('UI1: tag bump', () => {
  test('the row names the service and its old→new tag', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);

    skipper.setStackImage('web', '1.26');
    expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);

    const row = successRow(page, 'web');
    const d = delta(page, 'web');
    await expect(d).toBeVisible();
    const chip = d.locator('.tag-delta');
    await expect(chip).toHaveCount(1);
    await expect(chip.locator('.td-svc')).toHaveText('app');
    await expect(chip.locator('.td-old')).toHaveText('1.25');
    await expect(chip.locator('.td-new')).toHaveText('1.26');
    // Screen-reader label announces the change as one phrase, and the title
    // carries the full old→new reference (progressive disclosure).
    await expect(chip).toHaveAttribute('aria-label', 'app updated from 1.25 to 1.26');
    await expect(chip).toHaveAttribute('title', 'app: nginx:1.25 → nginx:1.26');
    // It lives in the row's own Version column — right of the Stack cell, and
    // aligned with the column header, which is what makes versions line up.
    await expect(row.locator('.col-version')).toContainText('1.26');
    const nameBox = await row.locator('.stack-name').boundingBox();
    const deltaBox = await d.boundingBox();
    const headBox = await page.locator('.event-list-header .col-version').boundingBox();
    expect(nameBox && deltaBox && headBox).toBeTruthy();
    expect(deltaBox!.x).toBeGreaterThan(nameBox!.x + nameBox!.width); // its own column
    expect(Math.abs(deltaBox!.x - headBox!.x)).toBeLessThan(4); // flush with the Version header
  });
});

// UI2 — the service is always named, even a lone service named after its own
// stack: the chip identifies which service moved, not the stack. Two bumps land
// it: the first renames the service to the stack, the second is a clean one-
// service tag bump.
test.describe('UI2: service always labelled', () => {
  test('a lone stack-named service still shows its service label', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);

    // The first push renames the sole service to the stack (app → worker); wait
    // for it to settle (the app-removed chip) so the second push is a clean
    // one-service tag bump and not coalesced with it.
    skipper.setStackServices('worker', { worker: 'nginx:2.0' });
    expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);
    await expect(delta(page, 'worker')).toContainText('removed');
    skipper.setStackServices('worker', { worker: 'nginx:2.1' });
    expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);

    // The newest worker row is the clean single-service tag bump.
    const chip = delta(page, 'worker').locator('.tag-delta');
    await expect(chip).toHaveCount(1);
    await expect(chip.locator('.td-svc')).toHaveText('worker'); // labelled even though it equals the stack
    await expect(chip.locator('.td-old')).toHaveText('2.0');
    await expect(chip.locator('.td-new')).toHaveText('2.1');
  });
});

// UI6 — the Deploys view-options carries a per-browser toggle that hides the
// delta; the choice (only the off state) persists in localStorage across reloads.
test.describe('UI6: view-options toggle', () => {
  const openDeploysMenu = (page: Page) => page.locator('#view-toggle button[data-view="deploys"]').click();
  const anyDelta = (page: Page) => page.locator('[data-testid="svc-delta"]');

  test('the toggle hides the delta and the off choice persists across reload', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);
    skipper.setStackImage('web', '1.26');
    expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);
    await expect(delta(page, 'web')).toBeVisible();

    const header = page.locator('.event-list-header .col-version');
    await expect(header).toBeVisible();
    // The row separators belong to the column: they exist because a Version cell
    // makes rows variable-height, so they come and go with it.
    const separated = () =>
      page
        .locator('[data-testid="deploy-row"]')
        .nth(1)
        .evaluate((r) => getComputedStyle(r).borderTopWidth !== '0px');
    expect(await separated()).toBe(true);

    // Flip it off from the popover: the whole column collapses — no chips left,
    // the Version header goes with it (an empty column would keep its width), and
    // the separators go too (every row is a single line again).
    await openDeploysMenu(page);
    const toggle = page.getByTestId('image-delta-toggle');
    await expect(toggle).toHaveClass(/active/); // on by default
    await toggle.click();
    await expect(anyDelta(page)).toHaveCount(0);
    await expect(header).toBeHidden();
    expect(await separated()).toBe(false);

    // The off choice survives a reload.
    await page.reload();
    await expect(page.locator('[data-testid="deploy-row"]').first()).toBeVisible();
    await expect(anyDelta(page)).toHaveCount(0);
    await expect(header).toBeHidden();

    // Toggling back on restores column, chips and separators (and clears the
    // stored override).
    await openDeploysMenu(page);
    await page.getByTestId('image-delta-toggle').click();
    await expect(delta(page, 'web')).toBeVisible();
    await expect(header).toBeVisible();
    expect(await separated()).toBe(true);
  });
});

// UI3 — a many-service deploy lists every changed service, one chip per line: the
// column is the answer to "what moved", so nothing is hidden behind a count.
test.describe('UI3: every changed service listed', () => {
  test('four changed services render four chips, stacked one per line', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);

    // app: tag bump; cache/queue/proxy: first image — four image changes.
    skipper.setStackServices('api', {
      app: 'nginx:1.26',
      cache: 'redis:7',
      queue: 'busybox:1.36',
      proxy: 'alpine:3.19',
    });
    expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);

    const d = delta(page, 'api');
    await expect(d).toBeVisible();
    const chips = d.locator('.tag-delta');
    await expect(chips).toHaveCount(4);
    await expect(chips.locator('.td-svc')).toHaveText(['app', 'cache', 'proxy', 'queue']);
    // Stacked: each chip sits below the previous one, sharing a left edge.
    const boxes = await chips.evaluateAll((els) => els.map((e) => e.getBoundingClientRect()).map((r) => ({ x: r.x, y: r.y })));
    for (let i = 1; i < boxes.length; i++) {
      expect(boxes[i].y).toBeGreaterThan(boxes[i - 1].y);
      expect(Math.abs(boxes[i].x - boxes[0].x)).toBeLessThan(2);
    }
  });
});

// UI4 — a same-tag rebuild (only the pinned digest moves) shows the shared tag
// plus a ↻ rebuilt marker (not two unreadable hex digests); the full digests
// are on the chip's title/aria.
test.describe('UI4: digest-only rebuild', () => {
  test('a same-tag digest change shows a rebuilt marker, not raw digests', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);

    const digest = (hex: string) => `nginx:1.25@sha256:${hex.repeat(64).slice(0, 64)}`;
    skipper.setStackServices('database', { app: digest('1') });
    expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);
    await expect(delta(page, 'database')).toBeVisible(); // first rebuild settled
    skipper.setStackServices('database', { app: digest('2') });
    expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);

    const chip = delta(page, 'database').locator('.tag-delta');
    await expect(chip).toHaveAttribute('title', /sha256:2222/); // newest row (its full digest in the title)
    await expect(chip).toHaveCount(1);
    await expect(chip.locator('.td-ctx')).toHaveText('1.25'); // shared tag
    await expect(chip.locator('.td-rebuilt')).toBeVisible(); // ↻ rebuilt marker
    await expect(chip.locator('.td-old')).toHaveCount(0); // no raw digest pair
  });
});

// UI5 — a phone has no columns to spare, so the row is two lines: name and
// status on the first, time · version on the second. The version rides as high
// as the width allows (Duration and Files are dropped for it, as on a tablet)
// without ever squeezing the name, the row's identity.
test.describe('UI5: responsive', () => {
  test('the Version cell shares the phone row\'s second line with the time', async ({ page, skipper }) => {
    await page.setViewportSize({ width: 390, height: 800 });
    await page.goto(`${skipper.baseURL}/`);

    skipper.setStackImage('web', '1.26');
    expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);

    const row = successRow(page, 'web');
    const d = delta(page, 'web');
    await expect(d).toBeVisible();
    // One measurement pass over the settled row: reading each box through its
    // own locator re-resolves `.first()` every time, and a row prepended between
    // two reads would have them describe different rows.
    const { nameBox, timeBox, deltaBox } = await row.evaluate((r) => {
      const box = (sel: string) => {
        const el = r.querySelector(sel) as HTMLElement;
        const b = el.getBoundingClientRect();
        return { x: b.x, y: b.y, width: b.width, height: b.height };
      };
      return {
        nameBox: box('.stack-name'),
        timeBox: box('[data-testid="time-cell"]'),
        deltaBox: box('[data-testid="svc-delta"]'),
      };
    });
    // Second line: below the name, beside the time rather than under it.
    expect(deltaBox.y).toBeGreaterThan(nameBox.y + nameBox.height - 2);
    expect(Math.abs(deltaBox.y - timeBox.y)).toBeLessThan(6);
    expect(deltaBox.x).toBeGreaterThan(timeBox.x + timeBox.width - 2);
    // Duration and Files give up their space for it (both stay in the panel).
    await expect(row.locator('.cell-duration')).toBeHidden();
    await expect(row.locator('.col-files')).toBeHidden();
    // The name still renders in full (nothing was squeezed to make room).
    const clipped = await row.locator('.stack-name').evaluate((el) => el.scrollWidth > el.clientWidth + 1);
    expect(clipped).toBe(false);
  });
});

// UI7 — a tablet has columns, just not six of them. Squeezing all six left the
// Version track ~80px, which wrapped every chip over three lines and let the
// stack cell print straight over them; so Duration and Files give up their
// tracks instead and Version keeps its place on the first line, in front of the
// status, where a column of versions can still be scanned down the page.
test.describe('UI7: tablet', () => {
  // Seeded health gives the status cell its second line, the widest state the
  // row reaches — the layout has to hold with it.
  test.use({
    startOptions: {
      stacks: ['web', 'api', 'worker', 'database'],
      healthPoll: 1,
      initialHealth: { api: [{ Service: 'app', State: 'running', Health: 'healthy' }] },
    },
  });

  test('Version keeps the first line while Duration and Files give up theirs', async ({ page, skipper }) => {
    await page.setViewportSize({ width: 744, height: 1000 }); // iPad-mini portrait
    await page.goto(`${skipper.baseURL}/`);

    // Two services so the column's own stacking is observable.
    skipper.setStackServices('api', { app: 'nginx:1.26', cache: 'redis:7' });
    expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);

    const row = successRow(page, 'api');
    const d = delta(page, 'api');
    await expect(d).toBeVisible();

    // Version stays a real column, header included; Duration and Files are the
    // two that go — from the rows and from the header, so the four that remain
    // stay in lockstep.
    await expect(page.locator('.event-list-header .col-version')).toBeVisible();
    await expect(page.locator('.event-list-header .eh-dur')).toBeHidden();
    await expect(page.locator('.event-list-header .eh-files')).toBeHidden();
    await expect(row.locator('.cell-duration')).toBeHidden();
    await expect(row.locator('.col-files')).toBeHidden();

    // On the first line, between the name and the status. One measurement pass
    // over the settled row: reading each box through its own locator re-resolves
    // `.first()` every time, and a row prepended between two reads would have
    // them describe different rows.
    const chips = d.locator('.tag-delta');
    await expect(chips).toHaveCount(2);
    const { nameBox, stackBox, statusBox, deltaBox, chipBoxes } = await row.evaluate((r) => {
      const rect = (el: Element) => {
        const b = el.getBoundingClientRect();
        return { x: b.x, y: b.y, width: b.width, height: b.height };
      };
      const box = (sel: string) => rect(r.querySelector(sel) as HTMLElement);
      return {
        nameBox: box('.stack-name'),
        stackBox: box('.cell-stack'),
        statusBox: box('.status-cell'),
        deltaBox: box('[data-testid="svc-delta"]'),
        chipBoxes: [...r.querySelectorAll('.tag-delta')].map(rect),
      };
    });
    expect(Math.abs(deltaBox.y - nameBox.y)).toBeLessThan(6);
    expect(deltaBox.x).toBeGreaterThan(stackBox.x + stackBox.width - 2);
    expect(deltaBox.x + deltaBox.width).toBeLessThanOrEqual(statusBox.x + 1);

    // Still a column: the chips stack one per line, as they do on a desktop.
    expect(chipBoxes[1].y).toBeGreaterThan(chipBoxes[0].y);
    expect(Math.abs(chipBoxes[1].x - chipBoxes[0].x)).toBeLessThan(2);

    // The name keeps its floor rather than being squeezed away by the cell's
    // own affordances — the row must never render an icon cluster with no stack
    // behind it.
    await expect(row.locator('.stack-name')).toHaveText('api');
    expect(nameBox.width).toBeGreaterThan(0);
  });
});
