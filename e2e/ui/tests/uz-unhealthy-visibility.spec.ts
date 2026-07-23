import { test, expect } from '../fixtures/test';
import type { Page } from '@playwright/test';

// Maske Z: unhealthy-stack visibility — the header health beacon and the Deploys
// attention band (ADR-0027 extension). Both read the same live health snapshot
// via attentionStacks(): a stack that rolls up to `unhealthy` is lifted out of
// the chronological deploy log, where its row-bound pill can sit far down or age
// out entirely. The health poller is enabled (`healthPoll: 1`) and each stack's
// `docker compose ps` output is scripted via the stub, so these are deterministic
// and offline. Behaviour-only: both surfaces are hidden whenever nothing is
// unhealthy, so no existing snapshot baseline changes. See dev-docs/e2e-tests.md
// §4.26.

const beacon = (page: Page) => page.locator('[data-testid="health-beacon"]');
const beaconCount = (page: Page) => page.locator('[data-testid="health-beacon-count"]');
const beaconPop = (page: Page) => page.locator('[data-testid="health-beacon-pop"]');
const band = (page: Page) => page.locator('[data-testid="attention-band"]');
const bandRows = (page: Page) => page.locator('[data-testid="attention-row"]');
const deployRow = (page: Page, stack: string) =>
  page.locator(`[data-testid="deploy-row"][data-stack="${stack}"]`);
const viewBtn = (page: Page, view: string) =>
  page.locator(`[data-testid="view-toggle"] button[data-view="${view}"]`);

// UZ1 — with two stacks unhealthy the beacon shows a pulsing count of 2, and its
// popover lists exactly those stacks; a healthy stack contributes nothing.
test.describe('UZ1: header beacon counts unhealthy stacks', () => {
  test.use({
    startOptions: {
      stacks: ['web', 'api', 'worker'],
      healthPoll: 1,
      initialHealth: {
        web: [{ Service: 'app', State: 'running', Health: 'healthy' }],
        api: [{ Service: 'app', State: 'running', Health: 'unhealthy' }],
        worker: [{ Service: 'app', State: 'restarting', Health: 'unhealthy' }],
      },
    },
  });

  test('the beacon shows the count and lists the unhealthy stacks', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);

    await expect(beacon(page)).toBeVisible();
    await expect(beaconCount(page)).toHaveText('2');
    await expect(beacon(page)).toHaveAttribute('aria-label', '2 stacks unhealthy');

    // The popover opens on click and lists exactly the two unhealthy stacks.
    await expect(beaconPop(page)).toBeHidden();
    await beacon(page).click();
    await expect(beaconPop(page)).toBeVisible();
    const items = beaconPop(page).locator('[data-testid="health-beacon-item"]');
    await expect(items).toHaveCount(2);
    await expect(items.filter({ hasText: 'api' })).toHaveCount(1);
    await expect(items.filter({ hasText: 'worker' })).toHaveCount(1);
    await expect(items.filter({ hasText: 'web' })).toHaveCount(0);
  });
});

// UZ2 — the attention band lists the unhealthy stack above the log and, on click,
// jumps to that stack's newest deploy row (the same jump the compass button does).
test.describe('UZ2: attention band jumps to the stack', () => {
  test.use({
    startOptions: {
      stacks: ['web', 'api'],
      healthPoll: 1,
      initialHealth: {
        web: [{ Service: 'app', State: 'running', Health: 'healthy' }],
        api: [{ Service: 'app', State: 'running', Health: 'unhealthy' }],
      },
    },
  });

  test('the band lists the unhealthy stack and lands on its newest deploy row', async ({
    page,
    skipper,
  }) => {
    await page.goto(`${skipper.baseURL}/`);

    await expect(band(page)).toBeVisible();
    await expect(bandRows(page)).toHaveCount(1);
    const apiRow = bandRows(page).filter({ hasText: 'api' });
    await expect(apiRow).toHaveCount(1);
    await expect(apiRow.locator('.health-pill')).toHaveAttribute('data-health', 'unhealthy');

    // Clicking a band row jumps to that stack's newest deploy row (temporary flash).
    await apiRow.click();
    const target = deployRow(page, 'api');
    await expect(target).toHaveClass(/jump-target/);
    await expect(target).not.toHaveClass(/jump-target/, { timeout: 3000 });
  });
});

// UZ3 — the beacon lives in the header (present in every view); the band belongs
// to the Deploys view. When the last unhealthy stack recovers, both disappear.
test.describe('UZ3: cross-view presence and recovery', () => {
  test.use({
    startOptions: {
      stacks: ['web', 'api'],
      healthPoll: 1,
      initialHealth: {
        web: [{ Service: 'app', State: 'running', Health: 'healthy' }],
        api: [{ Service: 'app', State: 'running', Health: 'unhealthy' }],
      },
    },
  });

  test('the beacon survives a view switch; recovery hides beacon and band', async ({
    page,
    skipper,
  }) => {
    await page.goto(`${skipper.baseURL}/`);
    await expect(beacon(page)).toBeVisible();
    await expect(band(page)).toBeVisible();

    // Switch to Stacks: the header beacon stays, the Deploys-scoped band hides.
    await viewBtn(page, 'stacks').click();
    await expect(page.locator('[data-testid="stacks-view"]')).toBeVisible();
    await expect(beacon(page)).toBeVisible();
    await expect(band(page)).toBeHidden();

    // Back to Deploys, then api recovers → nothing unhealthy → both vanish.
    await viewBtn(page, 'deploys').click();
    skipper.setStackHealth('api', [{ Service: 'app', State: 'running', Health: 'healthy' }]);
    await expect(beacon(page)).toBeHidden();
    await expect(band(page)).toBeHidden();
  });
});

// UZ4 — the Stacks view has no band; instead the roster floats the unhealthy
// stack to the top (a stable sort keeps the rest alphabetical), and a beacon
// jump lands on that roster row rather than yanking the user over to Deploys.
test.describe('UZ4: Stacks roster floats unhealthy first', () => {
  test.use({
    startOptions: {
      stacks: ['alpha', 'omega', 'mid'],
      healthPoll: 1,
      initialHealth: {
        alpha: [{ Service: 'app', State: 'running', Health: 'healthy' }],
        omega: [{ Service: 'app', State: 'running', Health: 'unhealthy' }],
        mid: [{ Service: 'app', State: 'running', Health: 'healthy' }],
      },
    },
  });

  test('the unhealthy roster row sorts first and the beacon jumps within Stacks', async ({
    page,
    skipper,
  }) => {
    await page.goto(`${skipper.baseURL}/`);
    await viewBtn(page, 'stacks').click();
    await expect(page.locator('[data-testid="stacks-view"]')).toBeVisible();

    // Alphabetical would be alpha, mid, omega — but unhealthy `omega` floats up,
    // and the healthy remainder keeps its alphabetical order.
    const rosterRows = page.locator('[data-testid="roster-row"]');
    await expect(rosterRows.nth(0)).toHaveAttribute('data-stack', 'omega');
    await expect(rosterRows.nth(1)).toHaveAttribute('data-stack', 'alpha');
    await expect(rosterRows.nth(2)).toHaveAttribute('data-stack', 'mid');

    // The beacon is present in Stacks too; its jump lands on the roster row.
    await expect(beacon(page)).toBeVisible();
    await beacon(page).click();
    await beaconPop(page).locator('[data-testid="health-beacon-item"]').filter({ hasText: 'omega' }).click();
    await expect(page.locator('[data-testid="stacks-view"]')).toBeVisible(); // stayed in Stacks
    const target = page.locator('[data-testid="roster-row"][data-stack="omega"]');
    await expect(target).toHaveClass(/jump-target/);
    await expect(target).not.toHaveClass(/jump-target/, { timeout: 3000 });
  });
});
