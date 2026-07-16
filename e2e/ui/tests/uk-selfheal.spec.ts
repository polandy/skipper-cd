import { test, expect } from '../fixtures/test';

// Maske K: Self-heal (ADR-0029). See dev-docs/e2e-tests.md §4.12.
//
// Drives the *real* self-heal loop through the running binary: the health poller
// (healthPoll: 1) reports a stack degraded via the stub docker's scripted `ps`
// output, and self-heal restores it with a corrective `up`. `initialHealth`
// seeds each stack healthy before boot so the first poll does not read the
// freshly-deployed-but-unscripted stack as `stopped` and heal it spuriously.
// min_unhealthy_polls / cooldown are lowered so the loop resolves in a couple of
// polls. Behaviour-only (no snapshot).

const healthyApp = [{ Service: 'app', State: 'running', Health: 'healthy' }];
const unhealthyApp = [{ Service: 'app', State: 'running', Health: 'unhealthy' }];

const webRow = (page: import('@playwright/test').Page, status: string) =>
  page.locator(`[data-testid="deploy-row"][data-stack="web"][data-status="${status}"]`);

// UK1 — a stack the poller finds unhealthy is restored by a corrective redeploy,
// surfacing as a `healed` row. Max attempts left high so it never exhausts here.
test.describe('UK1: self-heal restores a degraded stack', () => {
  test.use({
    startOptions: {
      stacks: ['web'],
      healthPoll: 1,
      selfHeal: true,
      selfHealMinUnhealthyPolls: 1,
      selfHealCooldownSeconds: 1,
      selfHealMaxAttempts: 5,
      initialHealth: { web: healthyApp },
    },
  });

  test('an unhealthy stack gets a corrective redeploy and a healed row', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);

    // Baseline: the startup deploy left one success row, no heal yet.
    await expect(webRow(page, 'success')).toHaveCount(1);
    await expect(webRow(page, 'healed')).toHaveCount(0);
    const upsBefore = skipper.dockerUps('web');

    // The stack turns unhealthy → the poller reports it → self-heal redeploys.
    skipper.setStackHealth('web', unhealthyApp);

    const healed = webRow(page, 'healed').first();
    await expect(healed).toBeVisible();
    await expect(healed.locator('[data-testid="status-badge"]')).toHaveText('healed');
    // The corrective `up` actually ran (beyond the startup one).
    await expect(() => expect(skipper.dockerUps('web')).toBeGreaterThan(upsBefore)).toPass();

    // Recovery quiesces the loop (no exhaustion).
    skipper.setStackHealth('web', healthyApp);
  });
});

// UK2 — when the redeploy cannot restore the stack (it stays unhealthy), the
// circuit breaker trips after max_attempts and emits a single heal_exhausted row
// with the give-up error. max_attempts: 1 → one heal, then exhausted.
test.describe('UK2: self-heal gives up after repeated failures', () => {
  test.use({
    startOptions: {
      stacks: ['web'],
      healthPoll: 1,
      selfHeal: true,
      selfHealMinUnhealthyPolls: 1,
      selfHealCooldownSeconds: 1,
      selfHealMaxAttempts: 1,
      initialHealth: { web: healthyApp },
    },
  });

  test('a stack that stays unhealthy exhausts self-heal into heal_exhausted', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);

    // Stays unhealthy across every poll: the one allowed heal does not fix it.
    skipper.setStackHealth('web', unhealthyApp);

    // The one permitted corrective redeploy still shows as a healed row…
    await expect(webRow(page, 'healed').first()).toBeVisible();

    // …then the breaker trips: a single heal_exhausted row with the alarm error.
    const exhausted = webRow(page, 'heal_exhausted').first();
    await expect(exhausted).toBeVisible();
    await expect(exhausted.locator('[data-testid="status-badge"]')).toContainText('self-heal');
    const errorPanel = page.locator('[data-testid="error-panel"][data-error-for="web"]');
    await expect(errorPanel).toContainText('self-heal exhausted');

    // Exactly one heal_exhausted — the give-up is emitted once, not per poll.
    await expect(webRow(page, 'heal_exhausted')).toHaveCount(1);
  });
});
