import { test, expect } from '../fixtures/test';
import type { Page } from '@playwright/test';

// Maske C: Autosync-Drawer. See docs/e2e-tests.md §4.4.

// UC1 — Header state. The autosync header control (`autosync-btn`) shows the
// *global* autosync state, and it is driven by the live `autosync` SSE event —
// not a `localStorage` preference. The button exposes that state as
// `data-global` ("true"/"false"), the machine-readable twin of its amber
// "paused" styling. We flip global autosync through the real API
// (`POST /api/autosync`) from a separate client, so the browser can only learn
// of the change via the server's broadcast `autosync` event; the header must
// mirror it live and again after a reload (proving it is server state, never
// persisted locally).
test.describe('UC1: header state', () => {
  const autosyncBtn = (page: Page) => page.locator('[data-testid="autosync-btn"]');

  test('the header control mirrors global autosync from the SSE event, across reload', async ({
    page,
    skipper,
  }) => {
    await page.goto(`${skipper.baseURL}/`);

    // Default config has global autosync on; the button reflects it once the
    // initial `autosync` snapshot streams in over SSE.
    await expect(autosyncBtn(page)).toHaveAttribute('data-global', 'true');

    // Pause globally via the real API (a separate client): the server publishes
    // an `autosync` event and the header flips live — no reload.
    expect(await skipper.postAutosync('', false)).toBe(200);
    await expect(autosyncBtn(page)).toHaveAttribute('data-global', 'false');

    // It is server state, not a `localStorage` toggle: a reload restores the
    // paused state from the server's snapshot, not from the browser.
    await page.reload();
    await expect(autosyncBtn(page)).toHaveAttribute('data-global', 'false');

    // Resuming flips it back on, live.
    expect(await skipper.postAutosync('', true)).toBe(200);
    await expect(autosyncBtn(page)).toHaveAttribute('data-global', 'true');
  });
});

// UC2 — Pending pill. The amber `pending-pill` on the header control is hidden
// when nothing is queued. Pausing autosync and then pushing a change defers the
// paused stack: the server registers it as pending and broadcasts a `queue`
// event, which makes the pill appear carrying the queue count. Resuming autosync
// drains the queue, so the registry empties and the pill hides again — the pill
// tracks live server queue depth, not a client guess.
test.describe('UC2: pending pill', () => {
  const pendingPill = (page: Page) => page.locator('[data-testid="pending-pill"]');

  test('the pending pill shows the queued count while paused and hides on drain', async ({
    page,
    skipper,
  }) => {
    await page.goto(`${skipper.baseURL}/`);

    // Nothing is queued at boot, so the pill is hidden.
    await expect(pendingPill(page)).toBeHidden();

    // Pause globally, then push a change: the paused `web` stack is deferred and
    // the pending registry (broadcast as a `queue` event) makes the pill appear
    // with the count.
    expect(await skipper.postAutosync('', false)).toBe(200);
    skipper.setStackImage('web', '1.26');
    expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);
    await expect(pendingPill(page)).toBeVisible();
    await expect(pendingPill(page)).toHaveText('1');

    // Resuming autosync drains the queue; the registry empties and the pill hides.
    expect(await skipper.postAutosync('', true)).toBe(200);
    await expect(pendingPill(page)).toBeHidden();
  });
});

// Shared drawer locators for the toggle-interaction cases.
const autosyncBtn = (page: Page) => page.locator('[data-testid="autosync-btn"]');
const globalSwitch = (page: Page) => page.locator('[data-testid="global-switch"]');
const stackSwitch = (page: Page, name: string) =>
  page.locator(`[data-testid="stack-switch"][data-stack="${name}"]`);
const stackItem = (page: Page, name: string) =>
  page.locator(`[data-testid="stack-item"][data-stack="${name}"]`);

// UC10 — Re-enable does not pin (override collapse). A per-stack UI override is an
// exception to the baseline, not a permanent pin (ADR-0019). Global is on; pausing
// a stack via its switch and then resuming it must leave *no* sticky override — a
// later global-off then pauses that stack along with the rest, proving the resume
// collapsed the override back to inherit rather than storing an explicit `true`.
// Driven entirely through the rendered switches, so the whole click→POST→SSE→DOM
// chain is exercised.
test.describe('UC10: re-enable does not pin', () => {
  test('pausing then resuming a stack leaves no sticky override', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);
    await autosyncBtn(page).click(); // open the drawer

    const web = stackSwitch(page, 'web');
    await expect(web).toHaveAttribute('aria-checked', 'true');

    // Pause, then resume the stack.
    await web.click();
    await expect(web).toHaveAttribute('aria-checked', 'false');
    await web.click();
    await expect(web).toHaveAttribute('aria-checked', 'true');

    // Turn global off: web must follow it. If the resume had pinned an explicit
    // override, web would stay syncing — the bug this guards against.
    await globalSwitch(page).click();
    await expect(globalSwitch(page)).toHaveAttribute('aria-checked', 'false');
    await expect(web).toHaveAttribute('aria-checked', 'false');
  });
});

// UC11 — A UI pause does not survive a global off→on cycle. Turning global off
// makes a UI-paused stack's baseline `off`, so its override collapses; turning
// global back on resumes it with everything else. This is the chosen master-switch
// semantic (ADR-0019): a UI pause is an exception relative to the current global
// baseline, not an independent latch. Durable pauses belong in `skipper.yml`.
test.describe('UC11: UI pause does not survive a global cycle', () => {
  test('a stack paused via the UI resumes after global off then on', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);
    await autosyncBtn(page).click();

    const web = stackSwitch(page, 'web');
    await web.click(); // pause web
    await expect(web).toHaveAttribute('aria-checked', 'false');

    const global = globalSwitch(page);
    await global.click(); // global off
    await expect(global).toHaveAttribute('aria-checked', 'false');
    await global.click(); // global back on
    await expect(global).toHaveAttribute('aria-checked', 'true');

    // The UI pause collapsed while global was off, so global-on resumes web too.
    await expect(web).toHaveAttribute('aria-checked', 'true');
  });
});

// UC12 — Override collapse through the stack filter. With several stacks and a
// filter narrowing the list, toggling a *filtered* stack must target the right
// stack and keep the query and matched subset intact; a global flip from a
// separate client re-renders the filtered list live over SSE without dropping the
// filter; and the collapse holds through the filtered/live view — a stack paused
// while filtered resumes after a global off→on cycle once the filter is cleared.
test.describe('UC12: collapse through the stack filter', () => {
  test.use({ startOptions: { stacks: ['web', 'api', 'db'] } });

  test('toggling a filtered stack preserves the filter and collapses correctly', async ({
    page,
    skipper,
  }) => {
    await page.goto(`${skipper.baseURL}/`);
    await autosyncBtn(page).click();

    // All three stacks are listed before filtering.
    await expect(stackItem(page, 'web')).toBeVisible();
    await expect(stackItem(page, 'api')).toBeVisible();
    await expect(stackItem(page, 'db')).toBeVisible();

    // Filter to just `api`; the others leave the DOM.
    await page.locator('[data-testid="stack-filter"]').fill('api');
    await expect(stackItem(page, 'api')).toBeVisible();
    await expect(stackItem(page, 'web')).toHaveCount(0);
    await expect(stackItem(page, 'db')).toHaveCount(0);

    // Pausing the filtered stack targets `api` and preserves the query/subset.
    await stackSwitch(page, 'api').click();
    await expect(stackSwitch(page, 'api')).toHaveAttribute('aria-checked', 'false');
    await expect(stackItem(page, 'web')).toHaveCount(0);
    await expect(stackItem(page, 'db')).toHaveCount(0);

    // Flip global OFF from a separate client: the filtered list re-renders live
    // over SSE, still showing only `api`.
    expect(await skipper.postAutosync('', false)).toBe(200);
    await expect(autosyncBtn(page)).toHaveAttribute('data-global', 'false');
    await expect(stackItem(page, 'api')).toBeVisible();
    await expect(stackItem(page, 'web')).toHaveCount(0);

    // Clear the filter: all three reappear, all paused (global off).
    await page.locator('[data-testid="stack-filter-clear"]').click();
    await expect(stackSwitch(page, 'web')).toHaveAttribute('aria-checked', 'false');
    await expect(stackSwitch(page, 'api')).toHaveAttribute('aria-checked', 'false');
    await expect(stackSwitch(page, 'db')).toHaveAttribute('aria-checked', 'false');

    // Global back ON: every stack resumes, including `api` — its UI pause collapsed
    // while global was off, so it is no longer a pinned exception.
    expect(await skipper.postAutosync('', true)).toBe(200);
    await expect(stackSwitch(page, 'web')).toHaveAttribute('aria-checked', 'true');
    await expect(stackSwitch(page, 'api')).toHaveAttribute('aria-checked', 'true');
    await expect(stackSwitch(page, 'db')).toHaveAttribute('aria-checked', 'true');
  });
});
