import { test, expect } from '../fixtures/test';
import type { Page, Locator } from '@playwright/test';

// Maske AI: per-service image delta on a deploy row. See docs/dev/e2e-tests.md
// §4.36.
//
// A deploy carries `image_changes` (service, old, new) on its SSE payload; the
// row's Change column surfaces which service updated — and to what — as one
// chip plus "+N" (its own line on a phone), without opening the diff. A webhook that rewrites a
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
    // It lives in the row's own Change column — right of the Stack cell, and
    // aligned with the column header, which is what makes versions line up.
    await expect(row.locator('.col-version')).toContainText('1.26');
    const nameBox = await row.locator('.stack-name').boundingBox();
    const deltaBox = await d.boundingBox();
    const headBox = await page.locator('.event-list-header .col-version').boundingBox();
    expect(nameBox && deltaBox && headBox).toBeTruthy();
    expect(deltaBox!.x).toBeGreaterThan(nameBox!.x + nameBox!.width); // its own column
    expect(Math.abs(deltaBox!.x - headBox!.x)).toBeLessThan(4); // flush with the Change header
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
    // Rows sit flush on one sheet, so the hairline between them is structural:
    // it is what says where a row ends, with or without the Change column.
    const separated = () =>
      page
        .locator('[data-testid="deploy-row"]')
        .nth(1)
        .evaluate((r) => getComputedStyle(r).borderBottomWidth !== '0px');
    expect(await separated()).toBe(true);

    // Flip it off from the popover: the whole column collapses — no chips left
    // and the Change header goes with it (an empty column would keep its
    // width) — while the row hairlines stay, since they are the sheet's, not
    // the column's.
    await openDeploysMenu(page);
    const toggle = page.getByTestId('image-delta-toggle');
    await expect(toggle).toHaveClass(/active/); // on by default
    await toggle.click();
    await expect(anyDelta(page)).toHaveCount(0);
    await expect(header).toBeHidden();
    expect(await separated()).toBe(true);

    // The off choice survives a reload.
    await page.reload();
    await expect(page.locator('[data-testid="deploy-row"]').first()).toBeVisible();
    await expect(anyDelta(page)).toHaveCount(0);
    await expect(header).toBeHidden();

    // Toggling back on restores the column and its chips (and clears the stored
    // override); the hairlines never went anywhere.
    await openDeploysMenu(page);
    await page.getByTestId('image-delta-toggle').click();
    await expect(delta(page, 'web')).toBeVisible();
    await expect(header).toBeVisible();
    expect(await separated()).toBe(true);
  });
});

// UI3 — a many-service deploy keeps the row to one chip plus a muted "+N",
// like the Stacks Version column; the row's detail lists every changed service.
test.describe('UI3: one chip plus a count', () => {
  test('four changed services render one chip and +3; the detail lists all four', async ({ page, skipper }) => {
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
    await expect(d.locator('.tag-delta')).toHaveCount(1);
    await expect(d.locator('.td-svc')).toHaveText('app');
    await expect(d.locator('[data-testid="change-more"]')).toHaveText('+3');

    await successRow(page, 'api').locator('.stack-name').click();
    const facts = page.locator('[data-testid="detail-panel"][data-kind="deploy"] [data-testid="detail-facts"]');
    await expect(facts.locator('.tag-delta .td-svc')).toHaveText(['app', 'cache', 'proxy', 'queue']);
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
// time on the first, change and result on the second, the right-hand cells
// right-aligned. The When cell drops its duration and SHA (both stay in the
// detail) without ever squeezing the name, the row's identity.
test.describe('UI5: responsive', () => {
  test('the Change cell opens the phone row\'s second line, beside the result', async ({ page, skipper }) => {
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
    const { nameBox, timeBox, deltaBox, statusBox } = await row.evaluate((r) => {
      const box = (sel: string) => {
        const el = r.querySelector(sel) as HTMLElement;
        const b = el.getBoundingClientRect();
        return { x: b.x, y: b.y, width: b.width, height: b.height };
      };
      return {
        nameBox: box('.stack-name'),
        timeBox: box('[data-testid="time-cell"]'),
        deltaBox: box('[data-testid="svc-delta"]'),
        statusBox: box('.status-cell'),
      };
    });
    // First line: name, then the time on the right.
    expect(Math.abs(timeBox.y - nameBox.y)).toBeLessThan(6);
    expect(timeBox.x).toBeGreaterThan(nameBox.x + nameBox.width - 2);
    // Second line: the change below the name, the result to its right.
    expect(deltaBox.y).toBeGreaterThan(nameBox.y + nameBox.height - 2);
    expect(statusBox.y).toBeGreaterThan(timeBox.y + timeBox.height - 2);
    expect(statusBox.x).toBeGreaterThan(deltaBox.x + deltaBox.width - 2);
    await expect(row.locator('.cell-duration')).toBeHidden();
    // The name still renders in full (nothing was squeezed to make room).
    const clipped = await row.locator('.stack-name').evaluate((el) => el.scrollWidth > el.clientWidth + 1);
    expect(clipped).toBe(false);
  });
});

// UI7 — a tablet keeps the four columns on one line: the When cell gives up
// its duration so Stack and Change keep their width, and the change still reads
// between the result and the time, where a column of changes can be scanned.
test.describe('UI7: tablet', () => {
  test('Change keeps the first line while When drops its duration', async ({ page, skipper }) => {
    await page.setViewportSize({ width: 744, height: 1000 }); // iPad-mini portrait
    await page.goto(`${skipper.baseURL}/`);

    skipper.setStackServices('api', { app: 'nginx:1.26', cache: 'redis:7' });
    expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);

    const row = successRow(page, 'api');
    const d = delta(page, 'api');
    await expect(d).toBeVisible();
    await expect(d.locator('[data-testid="change-more"]')).toHaveText('+1');

    await expect(page.locator('.event-list-header .col-version')).toBeVisible();
    await expect(row.locator('.cell-duration')).toBeHidden();

    // One measurement pass over the settled row (see UI5).
    const { nameBox, statusBox, deltaBox, timeBox } = await row.evaluate((r) => {
      const box = (sel: string) => {
        const b = (r.querySelector(sel) as HTMLElement).getBoundingClientRect();
        return { x: b.x, y: b.y, width: b.width, height: b.height };
      };
      return {
        nameBox: box('.stack-name'),
        statusBox: box('.status-cell'),
        deltaBox: box('[data-testid="svc-delta"]'),
        timeBox: box('[data-testid="time-cell"]'),
      };
    });
    // One line, in column order: name · result · change · time.
    for (const b of [statusBox, deltaBox, timeBox]) expect(Math.abs(b.y - nameBox.y)).toBeLessThan(8);
    expect(deltaBox.x).toBeGreaterThan(statusBox.x + statusBox.width - 2);
    expect(deltaBox.x + deltaBox.width).toBeLessThanOrEqual(timeBox.x + 1);

    // The name keeps its floor rather than being squeezed away.
    await expect(row.locator('.stack-name')).toHaveText('api');
    expect(nameBox.width).toBeGreaterThan(0);
  });
});
