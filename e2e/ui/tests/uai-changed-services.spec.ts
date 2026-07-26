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

// UI5 — a phone has no columns (the row is a 2×2 block), so the Version column
// becomes a full-width row beneath the name/time rather than squeezing the name.
test.describe('UI5: responsive', () => {
  test('the Version column becomes its own row on a phone', async ({ page, skipper }) => {
    await page.setViewportSize({ width: 390, height: 800 });
    await page.goto(`${skipper.baseURL}/`);

    skipper.setStackImage('web', '1.26');
    expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);

    const row = successRow(page, 'web');
    const d = delta(page, 'web');
    await expect(d).toBeVisible();
    const nameBox = await row.locator('.stack-name').boundingBox();
    const deltaBox = await d.boundingBox();
    expect(nameBox && deltaBox).toBeTruthy();
    // Below the name (its own row), starting at the row's left edge — not inline.
    expect(deltaBox!.y).toBeGreaterThan(nameBox!.y + nameBox!.height - 2);
    expect(deltaBox!.x).toBeLessThan(nameBox!.x);
    // The name still renders in full (nothing was squeezed to make room).
    const clipped = await row.locator('.stack-name').evaluate((el) => el.scrollWidth > el.clientWidth + 1);
    expect(clipped).toBe(false);
  });
});

// UI7 — a tablet still has columns, but not six of them: the Version track fell
// to ~80px there, which wrapped every chip over three lines and let the stack
// cell (which cannot shrink while a pending tag is beside it) print straight
// over the chips. Below 1000px the column drops to a full-width line of its own,
// the same move the phone makes one step further down.
test.describe('UI7: tablet', () => {
  test('the Version column drops to its own line and the row keeps its columns', async ({ page, skipper }) => {
    await page.setViewportSize({ width: 744, height: 1000 }); // iPad-mini portrait
    await page.goto(`${skipper.baseURL}/`);

    // Two services so the chips' own flow is observable.
    skipper.setStackServices('api', { app: 'nginx:1.26', cache: 'redis:7' });
    expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);

    const row = successRow(page, 'api');
    const d = delta(page, 'api');
    await expect(d).toBeVisible();

    // The column is gone from the header — the rows no longer have that track,
    // and an empty header cell would leave the remaining five out of lockstep.
    await expect(page.locator('.event-list-header .col-version')).toBeHidden();

    // Its own line, beneath the name and starting left of it.
    const nameBox = (await row.locator('.stack-name').boundingBox())!;
    const deltaBox = (await d.boundingBox())!;
    expect(nameBox && deltaBox).toBeTruthy();
    expect(deltaBox.y).toBeGreaterThan(nameBox.y + nameBox.height - 2);
    expect(deltaBox.x).toBeLessThan(nameBox.x);

    // On a full-width line the chips flow side by side instead of stacking (the
    // desktop column's one-per-line rule, UI3, is what the width paid for).
    const chips = d.locator('.tag-delta');
    await expect(chips).toHaveCount(2);
    const boxes = await chips.evaluateAll((els) => els.map((e) => e.getBoundingClientRect()).map((r) => ({ x: r.x, y: r.y })));
    expect(Math.abs(boxes[1].y - boxes[0].y)).toBeLessThan(2);
    expect(boxes[1].x).toBeGreaterThan(boxes[0].x);

    // The remaining columns still are columns: the stack cell stays inside its
    // track rather than printing over the status badge beside it.
    const stackBox = (await row.locator('.cell-stack').boundingBox())!;
    const statusBox = (await row.locator('.status-cell').boundingBox())!;
    expect(stackBox && statusBox).toBeTruthy();
    expect(stackBox.x + stackBox.width).toBeLessThanOrEqual(statusBox.x);
  });
});
