import { test, expect } from '../fixtures/test';
import type { Page } from '@playwright/test';

// Maske BC: the Stacks detail panel. See docs/dev/e2e-tests.md §4.56 and
// internal/ui/UI_SPEC.md "Stack detail panel".
//
// A roster row carries no controls: clicking it opens one detail panel under
// it — head (name + actions), verdict, facts, and one tab of Containers ·
// History · Triggers. The panel opens on the tab that answers the stack's state
// (UBC1, UBC2); the head's actions keep one order (UBC3); Logs and the Hooks
// pane work from the head (UBC4); the tabs switch and the verdict + facts read
// the stack (UBC5); the close button closes it (UBC6); a phone lays the facts in
// two columns (UBC7); a peer's panel is read-only (UBC8); and the tab strip
// follows the ARIA tabs pattern (UBC9, UBC10); a row opens and closes by
// keyboard (UBC11); the URL hash names the open detail and a loaded link opens
// it (UBC12-UBC14). Behaviour-only (no snapshot).

// web declares hooks and has an app link; api has neither; wip is parked.
const HOOKS_CONFIG = `stacks:
  web:
    hooks:
      pre_deploy:
        - "echo starting backup"
      post_deploy:
        - "echo verifying deploy"
  wip:
    disabled: true
`;
const HEALTHY = [{ Service: 'app', Image: 'nginx:1.27', State: 'running', Health: 'healthy' }];

const stacksBtn = (page: Page) => page.locator('[data-testid="view-toggle"] button[data-view="stacks"]');
const rosterRow = (page: Page, stack: string) =>
  page.locator(`[data-testid="roster-row"][data-stack="${stack}"]`);
const panel = (page: Page, stack: string) =>
  page.locator(`[data-testid="detail-panel"][data-stack="${stack}"]`);
const tab = (page: Page, stack: string, id: string) =>
  panel(page, stack).locator(`[data-testid="detail-tab"][data-tab="${id}"]`);

async function openStacks(page: Page, skipper: { baseURL: string }) {
  await page.goto(`${skipper.baseURL}/`);
  await stacksBtn(page).click();
  await expect(page.locator('[data-testid="stacks-view"]')).toBeVisible();
}

// The head's action testids in DOM order — the order the reader scans them.
async function headActions(page: Page, stack: string): Promise<string[]> {
  return panel(page, stack)
    .locator('.dp-actions')
    .evaluate((el) =>
      [...el.querySelectorAll('[data-testid]')]
        .map((e) => e.getAttribute('data-testid')!)
        .filter((id) => id !== 'app-link-pop'),
    );
}

test.describe('Maske BC: the Stacks detail panel', () => {
  test.use({
    startOptions: {
      stacks: ['web', 'api', 'wip'],
      discovery: { repoConfig: HOOKS_CONFIG, disabled: ['wip'] },
      healthPoll: 1, // health seeds the Containers tab; app-link detection rides it
      appLinks: { web: ['web.e2e.test'] },
      initialHealth: { web: HEALTHY, api: HEALTHY },
    },
  });

  // UBC1 — the panel opens on the tab that answers the stack's state: a healthy
  // stack on Containers, a parked one on Triggers (it runs nothing, and the
  // question is what it would deploy on).
  test('UBC1: a healthy stack opens on Containers, a parked one on Triggers', async ({ page, skipper }) => {
    await openStacks(page, skipper);

    await rosterRow(page, 'api').click();
    await expect(panel(page, 'api')).toHaveAttribute('data-tab', 'containers');
    await expect(tab(page, 'api', 'containers')).toHaveAttribute('aria-selected', 'true');
    await expect(panel(page, 'api').locator('[data-testid="health-panel"]')).toBeVisible();

    await rosterRow(page, 'wip').click();
    await expect(panel(page, 'wip')).toHaveAttribute('data-tab', 'triggers');
    await expect(panel(page, 'wip').locator('[data-testid="watched-panel"]')).toBeVisible();
    await expect(panel(page, 'wip').locator('.dp-v[data-kind="parked"]')).toBeVisible();
  });

  // UBC3 — the head's actions keep one order whatever the stack declares, so
  // a reader finds each in the same place; an action that does not apply is
  // absent, never a ghost.
  test('UBC3: the head actions keep their order, absent when they do not apply', async ({ page, skipper }) => {
    await openStacks(page, skipper);

    await rosterRow(page, 'web').click();
    // The app link needs a detection poll; reading before it lands would
    // compare a half-built head.
    await expect(panel(page, 'web').locator('[data-testid="app-link-btn"]')).toBeVisible();
    expect(await headActions(page, 'web')).toEqual([
      'app-link-btn',
      'clog-btn',
      'hooks-badge',
      'jump-btn',
      'detail-close',
    ]);
    await expect(panel(page, 'web').locator('[data-testid="hooks-badge"]')).toContainText('1+1');

    await rosterRow(page, 'api').click();
    await expect(panel(page, 'api')).toBeVisible();
    expect(await headActions(page, 'api')).toEqual(['clog-btn', 'jump-btn', 'detail-close']);
  });

  // UBC4 — Logs opens the live log panel; Hooks opens the hooks pane in place
  // of the tab and hands back the tab the reader came from, and choosing a tab
  // leaves the pane as well.
  test('UBC4: Logs opens the log panel; Hooks toggles the hooks pane', async ({ page, skipper }) => {
    await openStacks(page, skipper);
    await rosterRow(page, 'web').click();
    const p = panel(page, 'web');
    await expect(p).toHaveAttribute('data-tab', 'containers');

    await p.locator('.dp-actions [data-testid="clog-btn"]').click();
    await expect(page.locator('[data-testid="clog-panel"]')).toBeVisible();
    await expect(page.locator('[data-testid="clog-panel"]')).toContainText('web');

    const hooks = p.locator('[data-testid="hooks-badge"]');
    await hooks.click();
    await expect(p).toHaveAttribute('data-tab', 'hooks');
    await expect(p.locator('[data-testid="hooks-badge"]')).toHaveAttribute('aria-pressed', 'true');
    await expect(p.locator('[data-testid="hooks-panel"]')).toContainText('starting backup');
    await expect(p.locator('[data-testid="detail-tab"][aria-selected="true"]')).toHaveCount(0);

    await p.locator('[data-testid="hooks-badge"]').click();
    await expect(p).toHaveAttribute('data-tab', 'containers');
    await expect(p.locator('[data-testid="hooks-badge"]')).toHaveAttribute('aria-pressed', 'false');

    await p.locator('[data-testid="hooks-badge"]').click();
    await expect(p).toHaveAttribute('data-tab', 'hooks');
    await tab(page, 'web', 'triggers').click();
    await expect(p).toHaveAttribute('data-tab', 'triggers');
    await expect(p.locator('[data-testid="hooks-panel"]')).toHaveCount(0);
  });

  // UBC5 — the tabs switch the one pane; the verdict says the stack is fine
  // and the facts carry its four labels.
  test('UBC5: the tabs switch the pane; verdict and facts read the stack', async ({ page, skipper }) => {
    await openStacks(page, skipper);
    await rosterRow(page, 'api').click();
    const p = panel(page, 'api');

    await expect(p.locator('[data-testid="detail-verdict"] .dp-v[data-kind="ok"]')).toHaveAttribute(
      'data-tone',
      'ok',
    );
    await expect(p.locator('[data-testid="detail-verdict"]')).toContainText('Its service is healthy.');
    await expect(p.locator('[data-testid="detail-facts"] .dp-k')).toHaveText([
      'Running',
      'Last deploy',
      'Health',
      'Hooks',
    ]);
    await expect(p.locator('[data-testid="detail-facts"]')).toContainText('1 of 1 healthy');

    await tab(page, 'api', 'history').click();
    await expect(p).toHaveAttribute('data-tab', 'history');
    await expect(tab(page, 'api', 'history')).toHaveAttribute('aria-selected', 'true');
    await expect(p.locator('[data-testid="audit-panel"]')).toBeVisible();
    await expect(p.locator('[data-testid="health-panel"]')).toHaveCount(0);

    await tab(page, 'api', 'triggers').click();
    await expect(p.locator('[data-testid="watched-panel"]')).toBeVisible();
    await expect(p.locator('[data-testid="audit-panel"]')).toHaveCount(0);
  });

  // UBC6 — the close button closes the panel and releases the row; a second
  // row's click replaces the open panel rather than stacking a second one.
  test('UBC6: the close button closes; one panel at a time', async ({ page, skipper }) => {
    await openStacks(page, skipper);
    await rosterRow(page, 'api').click();
    await expect(rosterRow(page, 'api')).toHaveClass(/audit-open/);

    await rosterRow(page, 'web').click();
    await expect(page.locator('[data-testid="detail-panel"]')).toHaveCount(1);
    await expect(panel(page, 'web')).toBeVisible();

    await panel(page, 'web').locator('[data-testid="detail-close"]').click();
    await expect(page.locator('[data-testid="detail-panel"]')).toHaveCount(0);
    await expect(rosterRow(page, 'web')).not.toHaveClass(/audit-open/);
  });

  // UBC9 — the tab strip follows the ARIA tabs pattern: the arrows, Home and End
  // move the selection and the focus, only the selected tab is a Tab stop, and
  // the pane is labelled by the tab that shows it.
  test('UBC9: the arrow keys switch tabs and the pane names its tab', async ({ page, skipper }) => {
    await openStacks(page, skipper);
    await rosterRow(page, 'api').click();
    const p = panel(page, 'api');
    const pane = p.locator('[role="tabpanel"]');
    await expect(tab(page, 'api', 'containers')).toHaveAttribute('aria-selected', 'true');
    await expect(tab(page, 'api', 'containers')).toHaveAttribute('aria-controls', 'stack-detail-pane');
    await expect(pane).toHaveAttribute('id', 'stack-detail-pane');
    await expect(pane).toHaveAttribute('aria-labelledby', 'stack-detail-tab-containers');
    await expect(tab(page, 'api', 'history')).toHaveAttribute('tabindex', '-1');

    await tab(page, 'api', 'containers').focus();
    await page.keyboard.press('ArrowRight');
    await expect(tab(page, 'api', 'history')).toBeFocused();
    await expect(tab(page, 'api', 'history')).toHaveAttribute('aria-selected', 'true');
    await expect(tab(page, 'api', 'history')).toHaveAttribute('tabindex', '0');
    await expect(pane).toHaveAttribute('aria-labelledby', 'stack-detail-tab-history');
    await expect(p.locator('[data-testid="audit-panel"]')).toBeVisible();

    await page.keyboard.press('End');
    await expect(tab(page, 'api', 'triggers')).toBeFocused();
    await expect(p.locator('[data-testid="watched-panel"]')).toBeVisible();
    await page.keyboard.press('ArrowRight'); // wraps to the first tab
    await expect(tab(page, 'api', 'containers')).toBeFocused();
    await page.keyboard.press('ArrowLeft'); // and back to the last
    await expect(tab(page, 'api', 'triggers')).toBeFocused();
    await page.keyboard.press('Home');
    await expect(tab(page, 'api', 'containers')).toBeFocused();
    await expect(p.locator('[data-testid="health-panel"]')).toBeVisible();
  });

  // UBC10 — the head's Hooks pane selects no strip tab, so the pane takes its
  // own name and the first tab stays the strip's Tab stop.
  test('UBC10: the Hooks pane is named and the strip stays reachable', async ({ page, skipper }) => {
    await openStacks(page, skipper);
    await rosterRow(page, 'web').click();
    const p = panel(page, 'web');
    await p.locator('[data-testid="hooks-badge"]').click();
    await expect(p).toHaveAttribute('data-tab', 'hooks');
    await expect(p.locator('[role="tabpanel"]')).toHaveAttribute('aria-label', 'Hooks');
    await expect(p.locator('[data-testid="detail-tab"][tabindex="0"]')).toHaveCount(1);
    await expect(p.locator('[data-testid="detail-tab"]').first()).toHaveAttribute('tabindex', '0');
  });

  // UBC11 — the row's stack name is its keyboard toggle: Space opens the panel
  // and aria-expanded follows; Escape from inside the panel closes it and the
  // close button too hands the focus back to the toggle.
  test('UBC11: a row opens by keyboard and Escape closes it', async ({ page, skipper }) => {
    await openStacks(page, skipper);
    const toggle = rosterRow(page, 'api').locator('.row-toggle');
    await expect(toggle).toHaveAttribute('aria-expanded', 'false');

    await toggle.focus();
    await page.keyboard.press(' ');
    await expect(panel(page, 'api')).toBeVisible();
    await expect(toggle).toHaveAttribute('aria-expanded', 'true');

    await tab(page, 'api', 'containers').focus();
    await page.keyboard.press('Escape');
    await expect(panel(page, 'api')).toHaveCount(0);
    await expect(toggle).toHaveAttribute('aria-expanded', 'false');
    await expect(toggle).toBeFocused();

    await page.keyboard.press('Enter');
    await panel(page, 'api').locator('[data-testid="detail-close"]').focus();
    await page.keyboard.press('Enter');
    await expect(panel(page, 'api')).toHaveCount(0);
    await expect(toggle).toBeFocused();
  });
});

// UBC12, UBC13 — the URL hash follows the open Stacks detail, and loading it opens what it names.
test.describe('Maske BC: the deep link', () => {
  test.use({
    startOptions: {
      stacks: ['api', 'wip'],
      discovery: { repoConfig: 'stacks:\n  wip:\n    disabled: true\n', disabled: ['wip'] },
      healthPoll: 1,
      initialHealth: { api: HEALTHY },
    },
  });

  test('UBC12: the hash follows the open detail and its tab', async ({ page, skipper }) => {
    const root = `${skipper.baseURL}/`;
    await openStacks(page, skipper);
    await rosterRow(page, 'api').click();
    await expect(panel(page, 'api')).toHaveAttribute('data-tab', 'containers');
    await expect(page).toHaveURL(`${root}#stacks/api`);

    await tab(page, 'api', 'history').click();
    await expect(page).toHaveURL(`${root}#stacks/api?tab=history`);

    // Another view clears it; coming back restores the kept selection.
    await page.locator('[data-testid="view-toggle"] button[data-view="deploys"]').click();
    await expect(page).toHaveURL(root);
    await stacksBtn(page).click();
    await expect(panel(page, 'api')).toHaveAttribute('data-tab', 'history');
    await expect(page).toHaveURL(`${root}#stacks/api?tab=history`);

    await panel(page, 'api').locator('[data-testid="detail-close"]').click();
    await expect(page.locator('[data-testid="detail-panel"]')).toHaveCount(0);
    await expect(page).toHaveURL(root);
  });

  test('UBC13: loading a link opens the stack it names', async ({ page, skipper }) => {
    const root = `${skipper.baseURL}/`;
    await page.goto(`${root}#stacks/api?tab=history`);
    await expect(page.locator('[data-testid="stacks-view"]')).toBeVisible();
    await expect(panel(page, 'api')).toHaveAttribute('data-tab', 'history');
    await expect(tab(page, 'api', 'history')).toHaveAttribute('aria-selected', 'true');

    // An edited hash opens the new stack; an unknown tab falls back to the default and leaves the hash.
    await page.evaluate(() => (location.hash = '#stacks/wip?tab=nope'));
    await expect(panel(page, 'wip')).toHaveAttribute('data-tab', 'triggers');
    await expect(page).toHaveURL(`${root}#stacks/wip`);
  });
});

// UBC2 — a held change opens on History, the record of what failed, with the
// retry beside the verdict that names the hold (STUB_DOCKER_FAIL_NTH_UP=2:
// up#1 startup ok, up#2 fails, up#3 rollback ok).
test.describe('Maske BC: a held stack', () => {
  test.use({ startOptions: { stacks: ['web'], stubEnv: { STUB_DOCKER_FAIL_NTH_UP: '2' } } });

  test('UBC2: a held stack opens on History with its retry', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);
    const rolledBack = page.locator('[data-testid="deploy-row"][data-stack="web"][data-status="rolled_back"]');
    await expect(page.locator('[data-testid="deploy-row"][data-stack="web"]')).toHaveCount(1); // startup settled
    skipper.setStackImage('web', '1.26');
    expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);
    await expect(rolledBack).toHaveCount(1);
    await stacksBtn(page).click();
    // The hold is published with the run's roster snapshot, after the event.
    await expect(rosterRow(page, 'web').locator('[data-testid="held-chip"]')).toBeVisible();

    await rosterRow(page, 'web').click();
    const p = panel(page, 'web');
    await expect(p).toHaveAttribute('data-tab', 'history');
    await expect(p.locator('[data-testid="audit-panel"]')).toBeVisible();
    const held = p.locator('[data-testid="detail-verdict"] .dp-v[data-kind="held"]');
    await expect(held).toHaveAttribute('data-tone', 'rollback');
    await expect(held.locator('[data-testid="retry-btn"]')).toHaveText('Retry once');
  });
});

// UBC7 — on a phone the panel narrows rather than reflows: the head keeps its
// glyphs without labels, and the four facts sit in two columns of two.
test.describe('Maske BC: the detail panel on a phone', () => {
  test.use({
    viewport: { width: 448, height: 900 }, // Pixel 9 Pro, portrait
    startOptions: { stacks: ['api'], healthPoll: 1, initialHealth: { api: HEALTHY } },
  });

  test('UBC7: the facts sit in two columns and the actions drop their labels', async ({ page, skipper }) => {
    await openStacks(page, skipper);
    await rosterRow(page, 'api').click();
    const p = panel(page, 'api');
    await expect(p.locator('[data-testid="detail-facts"]')).toBeVisible();

    const facts = await p.locator('.dp-fact').evaluateAll((els) =>
      els.map((e) => {
        const b = e.getBoundingClientRect();
        return { x: Math.round(b.x), y: Math.round(b.y), right: Math.round(b.right) };
      }),
    );
    expect(facts).toHaveLength(4);
    // Two per line: 0 and 1 share a line, 2 and 3 the next, columns aligned.
    expect(facts[1].y).toBe(facts[0].y);
    expect(facts[3].y).toBe(facts[2].y);
    expect(facts[2].y).toBeGreaterThan(facts[0].y);
    expect(facts[2].x).toBe(facts[0].x);
    expect(facts[1].x).toBeGreaterThan(facts[0].x);

    // Glyph-only actions, every one inside the viewport.
    await expect(p.locator('.dp-actions .dp-lbl').first()).toBeHidden();
    const vw = page.viewportSize()!.width;
    const rights = await p
      .locator('.dp-actions [data-testid]')
      .evaluateAll((els) => els.map((e) => e.getBoundingClientRect().right));
    expect(rights.length).toBeGreaterThan(1);
    for (const r of rights) expect(r).toBeLessThanOrEqual(vw);
    expect(Math.max(...facts.map((f) => f.right))).toBeLessThanOrEqual(vw);
  });
});

// UBC8 — a peer's panel shows the peer's state read-only: its held verdict is
// there, but no retry (a retry is an action on another host) and no jump (the
// Deploys jump targets this host's rows).
test.describe('Maske BC: a peer stack', () => {
  const PEER_SHA = 'c'.repeat(40);
  test.use({
    startOptions: {
      stacks: ['api'],
      hostName: 'host-a',
      peers: [
        {
          name: 'host-b',
          snapshot: {
            stacks: {
              disabled: [],
              roster: [
                {
                  name: 'cache',
                  disabled: false,
                  last_status: 'rolled_back',
                  last_at: new Date().toISOString(),
                  last_commit: PEER_SHA,
                  held: { status: 'rolled_back', since: new Date().toISOString(), commit: PEER_SHA },
                },
              ],
            },
          },
        },
      ],
    },
  });

  test('UBC8: a peer panel shows the hold without retry or jump', async ({ page, skipper }) => {
    await openStacks(page, skipper);
    await page.locator('[data-testid="roster-row"][data-host="host-b"][data-stack="cache"]').click();

    const p = page.locator('[data-testid="detail-panel"].peer-panel[data-host="host-b"][data-stack="cache"]');
    await expect(p.locator('.dp-v[data-kind="held"]')).toBeVisible();
    await expect(p.locator('[data-testid="retry-btn"]')).toHaveCount(0);
    await expect(p.locator('[data-testid="jump-btn"]')).toHaveCount(0);
    await expect(p.locator('[data-testid="detail-close"]')).toBeVisible();
    await expect(page).toHaveURL(`${skipper.baseURL}/#stacks/host-b/cache`);
  });

  // UBC14 — a peer's link waits for the peer poll that brings its row.
  test('UBC14: a peer link opens once the peer row arrives', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/#stacks/host-b/cache`);
    const p = page.locator('[data-testid="detail-panel"].peer-panel[data-host="host-b"][data-stack="cache"]');
    await expect(p.locator('.dp-v[data-kind="held"]')).toBeVisible();
    await expect(page).toHaveURL(`${skipper.baseURL}/#stacks/host-b/cache`);
  });
});
