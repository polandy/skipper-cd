import { test, expect } from '../fixtures/test';
import type { Page } from '@playwright/test';

// Maske AA: Accessibility sweep (Tier 2 — T2.5–T2.10). See dev-docs/e2e-tests.md §4.28.
//
// Covers the keyboard/AT-facing behaviours the a11y sweep added: a keyboard
// focus ring on every control (T2.6), drawer focus management + trap (T2.7), an
// off-screen live region announcing terminal deploy outcomes (T2.8), and the
// host-row / host-mono keyboard operability + selection semantics (T2.10).
// Behaviour-only (no snapshot). The contrast retune (T2.5) and the touch-target
// overlays (T2.9) are pure CSS with no behavioural surface, so they are not
// asserted here.

const autosyncBtn = (page: Page) => page.locator('[data-testid="autosync-btn"]');
const autosyncDrawer = (page: Page) => page.locator('[data-testid="autosync-drawer"]');
const announce = (page: Page) => page.locator('[data-testid="a11y-announce"]');

const activeTestId = (page: Page) =>
  page.evaluate(() => document.activeElement?.getAttribute('data-testid') ?? null);

// UAA1 — Keyboard focus ring. Every interactive control carries a visible
// :focus-visible ring; the first keyboard Tab lands on a header control that
// must show an outline (the ring is suppressed for mouse focus, so this proves
// the keyboard path specifically). Guards against the pre-sweep state where
// glyph-only controls had no focus indicator at all.
test('UAA1: keyboard focus paints a visible ring on the focused control', async ({
  page,
  skipper,
}) => {
  await page.goto(`${skipper.baseURL}/`);

  // Move focus into the page by keyboard so :focus-visible engages.
  await page.keyboard.press('Tab');

  const outline = await page.evaluate(() => {
    const el = document.activeElement as HTMLElement | null;
    if (!el || el === document.body) return null;
    const s = getComputedStyle(el);
    return { style: s.outlineStyle, width: s.outlineWidth };
  });
  expect(outline).not.toBeNull();
  expect(outline!.style).not.toBe('none');
  expect(parseFloat(outline!.width)).toBeGreaterThan(0);
});

// UAA2 — Drawer focus management (T2.7). Opening the autosync drawer from the
// keyboard pulls focus inside it (onto the first control); Escape closes it and
// returns focus to the opener. The three role="dialog" drawers also declare
// aria-modal so AT treats them as modal.
test('UAA2: opening a drawer moves focus in and Escape returns it to the opener', async ({
  page,
  skipper,
}) => {
  await page.goto(`${skipper.baseURL}/`);

  await expect(autosyncDrawer(page)).toHaveAttribute('aria-modal', 'true');

  // Open via the keyboard (Enter on the focused control).
  await autosyncBtn(page).focus();
  await page.keyboard.press('Enter');
  await expect(autosyncDrawer(page)).toBeVisible();

  // Focus is pulled into the drawer — onto its first control, the global switch.
  await expect.poll(() => activeTestId(page)).toBe('global-switch');

  // Escape closes the drawer and hands focus back to the opener.
  await page.keyboard.press('Escape');
  await expect(autosyncDrawer(page)).toBeHidden();
  await expect.poll(() => activeTestId(page)).toBe('autosync-btn');
});

// UAA3 — Live region (T2.8). Terminal deploy outcomes are announced through an
// off-screen polite live region as they land live — but never on the history
// replay (which would read a backlog on every reconnect). The region is empty
// at boot (the startup deploy is history), then a live webhook deploy fills it.
test('UAA3: terminal deploy outcomes are announced live, not from history', async ({
  page,
  skipper,
}) => {
  await page.goto(`${skipper.baseURL}/`);
  await expect(page.locator('[data-testid="deploy-row"][data-stack="web"]')).toHaveCount(1);

  // Reload so the settled startup deploy now arrives as history: the replay must
  // be silent (announcing a backlog on every reconnect would be noise).
  await page.reload();
  await expect(page.locator('[data-testid="deploy-row"][data-stack="web"]')).toHaveCount(1);
  await expect(announce(page)).toHaveText('');

  // Wait for the gate to actually open rather than sleeping past its timer: the
  // live region publishes its state, so this is an assertion on the condition
  // the test depends on, not a guess at how long it takes.
  await expect(announce(page)).toHaveAttribute('data-announce-ready', '1');

  // A live change deploys `web` and the outcome is voiced.
  skipper.setStackImage('web', '1.26');
  expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);

  await expect(announce(page)).toHaveText('web deployed successfully');
});

// --- Multi-host host operability (T2.10) ---------------------------------
const hostB = {
  name: 'host-b',
  snapshot: {
    stacks: {
      roster: [
        { name: 'gitea', disabled: false, last_status: 'success', last_at: new Date().toISOString(), last_commit: 'aaa1111' },
      ],
      disabled: [],
    },
    health: { stacks: {} },
    app_links: { stacks: {} },
  },
  audit: [
    { stack: 'gitea', status: 'success', timestamp: new Date().toISOString(), duration_ms: 1400, changed_files: 1, commit_sha: 'aaa1111', id: 42 },
  ],
};

test.describe('host operability (multi-host)', () => {
  test.use({
    startOptions: { stacks: ['api', 'web'], hostName: 'host-a', peers: [hostB] },
  });

  const hostsBtn = (page: Page) => page.locator('[data-testid="hosts-btn"]');
  const hostsDrawer = (page: Page) => page.locator('[data-testid="hosts-drawer"]');
  const hostRow = (page: Page, name: string) =>
    page.locator(`[data-testid="host-row"][data-host="${name}"]`);
  const table = (page: Page) => page.locator('[data-testid="deploys-table"]');

  // UAA4 — Host-row selection semantics + keyboard. Each drawer host row is a
  // keyboard-operable checkbox: role="checkbox", aria-checked reflects whether
  // the host is in view, and Space toggles it (deselecting host-b narrows the
  // merged feed). Focus stays on the same host's row across the toggle rebuild.
  test('UAA4: host rows are keyboard-operable checkboxes with aria-checked', async ({
    page,
    skipper,
  }) => {
    await page.goto(`${skipper.baseURL}/`);
    await hostsBtn(page).click();
    await expect(hostsDrawer(page)).toHaveClass(/\bopen\b/);
    // The drawer auto-focuses its first host row on open (T2.7). Focus is placed
    // deterministically on open, so this asserts directly — no settle poll.
    await expect(hostsDrawer(page).locator('[data-testid="host-row"]').first()).toBeFocused();

    const b = hostRow(page, 'host-b');
    await expect(b).toHaveAttribute('role', 'checkbox');
    await expect(b).toHaveAttribute('aria-checked', 'true'); // all hosts in view at boot

    // Space on the focused row toggles it off — the peer's rows leave the feed.
    await b.focus();
    await page.keyboard.press(' ');
    await expect(hostRow(page, 'host-b')).toHaveAttribute('aria-checked', 'false');
    await expect(table(page)).toHaveClass(/host-filter-active/);
    // Focus was restored onto the rebuilt host-b row.
    await expect(hostRow(page, 'host-b')).toBeFocused();
  });

  // UAA5 — Host-mono chip keyboard. The per-row identity chip doubles as a
  // quick host filter and is now a keyboard-operable button (role/tabindex);
  // Enter on a focused chip isolates the view to that host, like a click.
  test('UAA5: the host-mono chip filters from the keyboard', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);
    const chip = page
      .locator('[data-testid="deploy-row"][data-host="host-b"]')
      .first()
      .locator('.host-mono');
    await expect(chip).toBeVisible();
    await expect(chip).toHaveAttribute('role', 'button');

    await chip.focus();
    await page.keyboard.press('Enter');
    await expect(table(page)).toHaveClass(/host-filter-active/);
  });
});
