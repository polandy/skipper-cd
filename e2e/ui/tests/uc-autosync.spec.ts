import { test, expect } from '../fixtures/test';
import type { Page } from '@playwright/test';
import { visualSnapshot } from '../fixtures/snapshot';

// Maske C: Autosync-Drawer. See dev-docs/e2e-tests.md §4.4.

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
const autosyncDrawer = (page: Page) => page.locator('[data-testid="autosync-drawer"]');

/** openDrawer opens the autosync drawer and waits until it carries server state.
 *  `data-ready` flips with the first `autosync` snapshot; before that the drawer
 *  is showing the markup's optimistic defaults and its global switch is inert
 *  (UC14), so a toggle asserted against it would be racing the stream. */
async function openDrawer(page: Page) {
  await autosyncBtn(page).click();
  await expect(autosyncDrawer(page)).toHaveAttribute('data-ready', 'true');
}

// UC3 — Drawer open/close. Clicking the `autosync-btn` header control opens the
// `autosync-drawer`; both `Esc` and an outside click close it again. The drawer
// starts hidden (`visibility: hidden`), so `toBeVisible`/`toBeHidden` track the
// `.open` class directly, and the button mirrors the state via `aria-expanded`.
test.describe('UC3: drawer open/close', () => {
  test('clicking opens the drawer; Esc and an outside click close it', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);

    // Closed at boot.
    await expect(autosyncDrawer(page)).toBeHidden();
    await expect(autosyncBtn(page)).toHaveAttribute('aria-expanded', 'false');

    // Click the control → the drawer opens and the button reports it.
    await autosyncBtn(page).click();
    await expect(autosyncDrawer(page)).toBeVisible();
    await expect(autosyncBtn(page)).toHaveAttribute('aria-expanded', 'true');

    // Esc closes it (the handler is bound document-wide).
    await page.keyboard.press('Escape');
    await expect(autosyncDrawer(page)).toBeHidden();
    await expect(autosyncBtn(page)).toHaveAttribute('aria-expanded', 'false');

    // Reopen, then click well outside the drawer and button → it closes again.
    await autosyncBtn(page).click();
    await expect(autosyncDrawer(page)).toBeVisible();
    await page.mouse.click(10, 400); // far from the top-right drawer/button
    await expect(autosyncDrawer(page)).toBeHidden();
    await expect(autosyncBtn(page)).toHaveAttribute('aria-expanded', 'false');
  });
});

const queueItem = (page: Page, name: string) =>
  page.locator(`[data-testid="queue-item"][data-stack="${name}"]`);
const stackFilter = (page: Page) => page.locator('[data-testid="stack-filter"]');
const stackFilterClear = (page: Page) => page.locator('[data-testid="stack-filter-clear"]');
const webDeployRow = (page: Page, status: string) =>
  page.locator(`[data-testid="deploy-row"][data-stack="web"][data-status="${status}"]`);

// UC4 — Global switch. Toggling the drawer's `global-switch` posts
// `POST /api/autosync {scope:"global"}` and the header autosync control mirrors
// the new state live. Driven entirely through the rendered switch: the click →
// POST → SSE → DOM chain flips both the switch (`aria-checked`) and the header
// (`data-global`), and a reload restores it from the server (proving the POST
// landed, not just a local class toggle).
test.describe('UC4: global switch', () => {
  test('toggling posts global scope and the header mirrors it, across reload', async ({
    page,
    skipper,
  }) => {
    await page.goto(`${skipper.baseURL}/`);
    await openDrawer(page);

    // On by default: switch and header agree.
    await expect(globalSwitch(page)).toHaveAttribute('aria-checked', 'true');
    await expect(autosyncBtn(page)).toHaveAttribute('data-global', 'true');

    // Click off → the header flips live with the switch.
    await globalSwitch(page).click();
    await expect(globalSwitch(page)).toHaveAttribute('aria-checked', 'false');
    await expect(autosyncBtn(page)).toHaveAttribute('data-global', 'false');

    // The POST reached the server: a reload restores `off` from its snapshot.
    await page.reload();
    await expect(autosyncBtn(page)).toHaveAttribute('data-global', 'false');

    // Click on again → back to on, live.
    await openDrawer(page);
    await globalSwitch(page).click();
    await expect(autosyncBtn(page)).toHaveAttribute('data-global', 'true');
  });
});

// UC5 — Per-stack switch. Toggling a `stack-switch` posts
// `POST /api/autosync {scope:"stack",stack}`; the paused state survives closing
// and reopening the drawer because it is re-read from the server snapshot on
// reopen, not held in the DOM.
test.describe('UC5: per-stack switch', () => {
  test('toggling posts stack scope and the state survives a drawer reopen', async ({
    page,
    skipper,
  }) => {
    await page.goto(`${skipper.baseURL}/`);
    await openDrawer(page);

    const web = stackSwitch(page, 'web');
    await expect(web).toHaveAttribute('aria-checked', 'true');

    // Pause the stack via its switch.
    await web.click();
    await expect(web).toHaveAttribute('aria-checked', 'false');

    // Close and reopen: the paused state is reflected from the server, not lost.
    await page.keyboard.press('Escape');
    await expect(autosyncDrawer(page)).toBeHidden();
    await openDrawer(page);
    await expect(stackSwitch(page, 'web')).toHaveAttribute('aria-checked', 'false');
  });
});

// UC6 — Queued list. Pausing globally and pushing changes to two stacks defers
// both; the drawer's queue list renders a `queue-item` per pending stack in
// deploy order, each carrying its position (`qpos`), a reason chip (`global`
// here), the changed-file count, and a wait time. Asserted through the rendered
// list against the real backend.
test.describe('UC6: queued list', () => {
  test.use({ startOptions: { stacks: ['web', 'api'] } });

  test('queued stacks render in order with position, reason, file count, and wait', async ({
    page,
    skipper,
  }) => {
    await page.goto(`${skipper.baseURL}/`);

    // Pause globally, then push a change to both stacks → both are deferred.
    expect(await skipper.postAutosync('', false)).toBe(200);
    skipper.setStackImage('web', '1.26');
    skipper.setStackImage('api', '1.26');
    expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);

    await openDrawer(page);
    await expect(queueItem(page, 'web')).toBeVisible();
    await expect(queueItem(page, 'api')).toBeVisible();

    // Positions are 1 and 2 in DOM (deploy) order — read them off the rendered
    // list without coupling to which stack sorts first.
    const positions = await page
      .locator('[data-testid="queue-item"] .qpos')
      .evaluateAll((els) => els.map((e) => e.textContent?.trim()));
    expect(positions).toEqual(['1', '2']);

    // Each queued row carries a global reason chip, a one-file count, and a wait
    // cell (the value itself is time-dependent, so only its presence is asserted).
    await expect(queueItem(page, 'web').locator('.reason-global')).toHaveText('global');
    await expect(queueItem(page, 'web')).toContainText('1 file');
    await expect(queueItem(page, 'web').locator('[data-testid="wait-cell"]')).toBeVisible();

    // Snapshot: the drawer with its ordered queue (§5 anchor), wait cells masked
    // (the elapsed time is inherently variable).
    await visualSnapshot(autosyncDrawer(page), 'autosync-drawer.png', {
      mask: [page.locator('[data-testid="wait-cell"]')],
    });
  });
});

// UC7 — Stack filter. Typing narrows the stack list by case-insensitive substring
// and reveals a clear button; a no-match query empties the list; the clear button
// restores it; and `Esc` clears a non-empty field first (leaving the drawer open),
// then closes the drawer on a second press.
test.describe('UC7: stack filter', () => {
  test.use({ startOptions: { stacks: ['web', 'api', 'db'] } });

  test('filters by substring, clears, and Esc clears before closing', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);
    await openDrawer(page);

    // All three listed; clear button hidden until there is a query.
    await expect(stackItem(page, 'web')).toBeVisible();
    await expect(stackItem(page, 'api')).toBeVisible();
    await expect(stackItem(page, 'db')).toBeVisible();
    await expect(stackFilterClear(page)).toBeHidden();

    // Case-insensitive substring: "AP" matches only `api`.
    await stackFilter(page).fill('AP');
    await expect(stackItem(page, 'api')).toBeVisible();
    await expect(stackItem(page, 'web')).toHaveCount(0);
    await expect(stackItem(page, 'db')).toHaveCount(0);
    await expect(stackFilterClear(page)).toBeVisible();

    // A query matching nothing empties the list.
    await stackFilter(page).fill('zzz');
    await expect(stackItem(page, 'web')).toHaveCount(0);
    await expect(stackItem(page, 'api')).toHaveCount(0);
    await expect(stackItem(page, 'db')).toHaveCount(0);

    // The clear button restores the full list and hides itself.
    await stackFilterClear(page).click();
    await expect(stackItem(page, 'web')).toBeVisible();
    await expect(stackItem(page, 'db')).toBeVisible();
    await expect(stackFilterClear(page)).toBeHidden();

    // Esc on a non-empty field clears it but keeps the drawer open…
    await stackFilter(page).fill('web');
    await stackFilter(page).press('Escape');
    await expect(stackFilter(page)).toHaveValue('');
    await expect(autosyncDrawer(page)).toBeVisible();
    // …and a second Esc (empty field now) closes the drawer.
    await stackFilter(page).press('Escape');
    await expect(autosyncDrawer(page)).toBeHidden();
  });
});

// UC8 — Enable drains, disable does not. Disabling autosync only updates state —
// it never runs a deploy. Enabling triggers a deploy run that drains the queue:
// the pending stack's `up` runs and the pending pill empties. Proven by the
// stub-docker `up` count and the pending pill against the real backend.
test.describe('UC8: enable drains, disable does not', () => {
  const pendingPill = (page: Page) => page.locator('[data-testid="pending-pill"]');

  test('enabling runs the queued deploy; disabling runs none', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);

    // The startup deploy already ran one `up`; pausing runs no further deploy.
    expect(skipper.dockerUps('web')).toBe(1);
    expect(await skipper.postAutosync('', false)).toBe(200);
    expect(skipper.dockerUps('web')).toBe(1);

    // Push a change while paused → queued, still no `up`, pill shows.
    skipper.setStackImage('web', '1.26');
    expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);
    await expect(pendingPill(page)).toBeVisible();
    expect(skipper.dockerUps('web')).toBe(1);

    // Enabling drains the queue: the deferred deploy runs (a second `up`) and the
    // pill hides.
    expect(await skipper.postAutosync('', true)).toBe(200);
    await expect.poll(() => skipper.dockerUps('web')).toBe(2);
    await expect(pendingPill(page)).toBeHidden();
  });
});

// UC9 — Queued row + tag. A `queued` event renders a `deploy-row` with the
// `queued` status badge and a `paused:` reason tag; resuming autosync deploys the
// stack and the queued row is superseded by a fresh success row (the Mask C twin
// of UA11, here asserting the badge + tag).
test.describe('UC9: queued row + tag', () => {
  test('a paused change renders a queued badge + paused tag, superseded on resume', async ({
    page,
    skipper,
  }) => {
    await page.goto(`${skipper.baseURL}/`);
    await expect(webDeployRow(page, 'success')).toHaveCount(1); // startup settled

    // Pause, then push a change → a queued row with the badge and the tag.
    expect(await skipper.postAutosync('', false)).toBe(200);
    skipper.setStackImage('web', '1.26');
    expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);

    await expect(webDeployRow(page, 'queued')).toHaveCount(1);
    await expect(webDeployRow(page, 'queued').locator('[data-testid="status-badge"]')).toHaveText(
      'queued',
    );
    // The row carries the `paused: global` tag. The reason arrives with the
    // `queue` snapshot, which is not ordered against the `queued` deploy event —
    // the UI refreshes the tag when the snapshot lands, so the full wording is
    // deterministic (the web-first assertion polls through the refresh).
    await expect(webDeployRow(page, 'queued').locator('.paused-tag')).toHaveText('paused: global');

    // Resume → the stack deploys and the queued row is superseded by success.
    expect(await skipper.postAutosync('', true)).toBe(200);
    await expect(webDeployRow(page, 'success')).toHaveCount(2);
    await expect(webDeployRow(page, 'queued')).toHaveCount(0);
  });
});

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
    await openDrawer(page);

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
    await openDrawer(page);

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
    await openDrawer(page);

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

// UC13 — A late snapshot never overwrites a newer one. Autosync state reaches the
// UI over two channels — the toggle's own POST response and the SSE broadcast the
// same change triggers — and they can overtake each other, so a switch could snap
// back to the state before the last change. Ordering therefore comes from the
// snapshot's monotonic `version`, not from arrival time (dev-docs/autosync-spec.md).
//
// The reversal is *forced*, not awaited: the POST is let through to the server so
// it really mutates, but its response is held in the route handler until a newer
// state has demonstrably been applied (asserted in the DOM). Releasing the stale
// response then either changes the switch — the bug — or is dropped.
test.describe('UC13: a late snapshot never overwrites a newer one', () => {
  test('a delayed POST response is dropped when newer state already landed', async ({
    page,
    skipper,
  }) => {
    let release = () => {};
    const held = new Promise<void>((resolve) => {
      release = resolve;
    });

    // Hold only the first POST response; later ones pass straight through.
    let holdNext = true;
    await page.route('**/api/autosync', async (route) => {
      if (route.request().method() !== 'POST' || !holdNext) return route.continue();
      holdNext = false;
      const response = await route.fetch(); // the server mutates and broadcasts now
      await held;
      await route.fulfill({ response });
    });

    await page.goto(`${skipper.baseURL}/`);
    await openDrawer(page);
    await expect(globalSwitch(page)).toHaveAttribute('aria-checked', 'true');

    // Click global off. Its POST response is held, but the server already applied
    // the change, so the SSE broadcast paints the switch off — version 1.
    await globalSwitch(page).click();
    await expect(globalSwitch(page)).toHaveAttribute('aria-checked', 'false');

    // A separate client turns global back on — version 2, applied over SSE.
    expect(await skipper.postAutosync('', true)).toBe(200);
    await expect(globalSwitch(page)).toHaveAttribute('aria-checked', 'true');

    // Now let the stale version-1 response (global off) arrive last. Waiting for
    // the *response* would not be enough: "the switch did not move" is an absence,
    // and asserting it right after delivery passes before the page has even
    // handled the payload — green whether or not the guard exists. The drop is
    // therefore announced, and that announcement is what we wait for. It is
    // emitted in the same synchronous step in which the switch would otherwise
    // have been repainted, so once it arrives the DOM assertions cannot be early.
    const dropped = page.waitForEvent('console', (m) =>
      m.text().includes('autosync: dropped a stale snapshot'),
    );
    release();
    await dropped;

    // The header control mirrors the same snapshot, so assert both surfaces.
    await expect(autosyncBtn(page)).toHaveAttribute('data-global', 'true');
    await expect(globalSwitch(page)).toHaveAttribute('aria-checked', 'true');
  });
});

// UC14 — The drawer never acts on state it does not have. Between page load and
// the first `autosync` snapshot the drawer only has the markup's optimistic
// defaults: the global switch *looks* on, but a click could not compute the
// opposite value and posted nothing at all — a dead control, and a lost toggle
// for whoever opened the drawer that early. The drawer now says so
// (`data-ready="false"`, `aria-disabled`) and the switch does not take the click
// until the state lands, so the click waits instead of vanishing. Driven by
// stalling the event stream, which makes that window last as long as we need.
test.describe('UC14: the drawer waits for server state', () => {
  test('the global switch is inert until the first snapshot, then takes the click', async ({
    page,
    skipper,
  }) => {
    let release = () => {};
    const stalled = new Promise<void>((resolve) => {
      release = resolve;
    });
    await page.route('**/api/events', async (route) => {
      await stalled;
      await route.continue();
    });

    await page.goto(`${skipper.baseURL}/`);
    await autosyncBtn(page).click();
    await expect(autosyncDrawer(page)).toBeVisible();

    // No snapshot has arrived, and the drawer admits it rather than presenting a
    // switch whose position it is guessing.
    await expect(autosyncDrawer(page)).toHaveAttribute('data-ready', 'false');
    await expect(globalSwitch(page)).toHaveAttribute('aria-disabled', 'true');

    // Click while it is still inert. The click must not be swallowed: it waits
    // for the control to arm, which happens when we let the stream through.
    const clicked = globalSwitch(page).click();
    release();
    await clicked;

    // The state arrived, the switch armed, and the click landed on the server.
    await expect(autosyncDrawer(page)).toHaveAttribute('data-ready', 'true');
    await expect(globalSwitch(page)).not.toHaveAttribute('aria-disabled', 'true');
    await expect(globalSwitch(page)).toHaveAttribute('aria-checked', 'false');
    await expect(autosyncBtn(page)).toHaveAttribute('data-global', 'false');
  });
});

// UC15 — A snapshot must not swap a switch out from under the pointer. Every
// `autosync`/`queue` event repaints the drawer's lists. Rebuilding them wholesale
// replaced the switch nodes, and a switch replaced between mousedown and mouseup
// takes the `click` with it — the browser fires it on the common ancestor
// instead, where the delegated handler finds no switch and does nothing. The
// lists are therefore patched in place while the rows and their order hold, which
// this asserts through node identity: the very node a click would be travelling
// to is still the live, connected control after an unrelated stack's snapshot.
test.describe('UC15: a re-render keeps the switch nodes', () => {
  test.use({ startOptions: { stacks: ['web', 'api'] } });

  test('an unrelated snapshot patches the list without replacing the switch', async ({
    page,
    skipper,
  }) => {
    await page.goto(`${skipper.baseURL}/`);
    await openDrawer(page);

    const web = stackSwitch(page, 'web');
    await expect(web).toHaveAttribute('aria-checked', 'true');
    const node = await web.elementHandle();

    // Pause a *different* stack from another client: the snapshot repaints the
    // whole list. Asserting `api` flipped proves the repaint actually ran, so the
    // node check below cannot pass by being early.
    expect(await skipper.postAutosync('api', false)).toBe(200);
    await expect(stackSwitch(page, 'api')).toHaveAttribute('aria-checked', 'false');

    // web's switch survived that repaint as the same node — nothing was swapped
    // out mid-flight.
    expect(await node!.evaluate((el) => el.isConnected)).toBe(true);

    // And it is still the live control, not a detached leftover.
    await web.click();
    await expect(web).toHaveAttribute('aria-checked', 'false');
  });
});
