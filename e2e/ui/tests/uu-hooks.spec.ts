import { test, expect } from '../fixtures/test';

// Maske U: deploy hooks in the UI (ADR-0038). See dev-docs/e2e-tests.md §4.22.
//
// Boots in discovery mode with a stack whose repo skipper.yaml declares
// pre/post-deploy hooks. The commands are harmless `echo`s (they succeed and
// their output is attributed to the stack in the log), plus a hook that blocks
// on the harness hook-hold file for the running-hook cases so the phase stays
// observable until the test releases it — no wall-clock race. No real docker;
// hooks run via real `sh -c`, so the stub docker is untouched.

// web has 2 pre + 1 post; the echoes succeed so the startup deploy settles.
const WEB_HOOKS = `stacks:
  web:
    hooks:
      pre_deploy:
        - "echo starting backup"
        - "echo dumping database"
      post_deploy:
        - "echo verifying deploy"
`;

// A pre_deploy hook that blocks on the harness hook-hold file so the
// running-hook phase stays observable until the test calls releaseHook() — no
// timing window that can close under CI load. readiness:'listening' loads the
// page while the deploy is parked in this hook.
const WEB_HELD = `stacks:
  web:
    hooks:
      pre_deploy:
        - "echo starting backup"
        - 'while [ ! -f "$SKIPPER_E2E_HOOK_HOLD" ]; do sleep 0.05; done'
`;

// UU1 — a stack with hooks shows the badge (split pre+post count); its panel
// lists the configured commands; a stack without hooks has no badge.
test.describe('UU1: hooks badge + panel', () => {
  test.use({ startOptions: { stacks: ['web', 'api'], discovery: { repoConfig: WEB_HOOKS } } });

  test('badge shows pre+post and opens a panel of the commands', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);

    const badge = page.locator('[data-testid="deploy-row"][data-stack="web"] [data-testid="hooks-badge"]');
    await expect(badge).toBeVisible();
    await expect(badge).toContainText('2+1'); // 2 pre-deploy, 1 post-deploy
    await expect(badge).toHaveAttribute('title', /pre-deploy hook: 2/);

    // api is discovered but declares no hooks → no badge.
    await expect(
      page.locator('[data-testid="deploy-row"][data-stack="api"] [data-testid="hooks-badge"]'),
    ).toHaveCount(0);

    await badge.click();
    const panel = page.locator('[data-testid="hooks-panel"]');
    await expect(panel).toBeVisible();
    const cmds = panel.locator('[data-testid="hooks-cmd"]');
    await expect(cmds).toHaveCount(3);
    await expect(cmds.nth(0)).toContainText('echo starting backup');
    await expect(cmds.nth(2)).toContainText('echo verifying deploy');

    // Toggling the badge closes the panel (one-panel-per-row lifecycle).
    await badge.click();
    await expect(panel).toHaveCount(0);
  });
});

// UU2 — the hook's stdout is attributed to its stack in the log view: the line
// carries a [web] prefix and the stack filter matches it.
test.describe('UU2: hook output attributed in the log', () => {
  test.use({ startOptions: { stacks: ['web'], discovery: { repoConfig: WEB_HOOKS } } });

  test('the echo hook output shows with a [web] prefix in the log', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);
    await page.locator('[data-testid="view-toggle"] button[data-view="logs"]').click();

    // Type-to-search filters the log to lines mentioning the stack.
    await page.keyboard.type('web', { delay: 20 });
    await expect(page.locator('[data-testid="log-filter"]')).toHaveValue('web');

    const line = page.locator('[data-testid="log-line"]').filter({ hasText: 'starting backup' });
    await expect(line).toBeVisible();
    await expect(line.locator('[data-testid="stack-prefix"]')).toHaveText('[web]');
  });
});

// UU3 — while a hook runs, the deploying row shows the phase + a pulsing badge,
// and the console icon opens the hook log inline (no page jump).
test.describe('UU3: running-hook phase + inline log', () => {
  test.use({
    startOptions: { stacks: ['web'], discovery: { repoConfig: WEB_HELD }, readiness: 'listening' },
  });

  test('the phase shows and the console icon opens an inline log panel', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);

    const row = page.locator('[data-testid="deploy-row"][data-stack="web"]');
    const phase = row.locator('[data-testid="hook-phase"]');
    await expect(phase).toBeVisible();
    await expect(phase).toContainText('pre_deploy hook');
    // The hooks badge pulses while a hook runs.
    await expect(row.locator('[data-testid="hooks-badge"]')).toHaveAttribute('data-hook-active', '1');

    // The console icon opens the container-logs panel in skipper mode, inline —
    // the deploys table stays visible (no jump to the log view).
    await phase.locator('[data-testid="clog-btn"]').click();
    const panel = page.locator('[data-testid="clog-panel"]');
    await expect(panel).toBeVisible();
    await expect(panel.locator('[data-testid="clog-body"]')).toContainText('starting backup');
    await expect(page.locator('[data-testid="deploys-table"]')).toBeVisible();

    skipper.releaseHook(); // let the held hook finish so the deploy settles and teardown is clean
  });
});

// UU4 — the running-hook phase renders identically in the Stacks roster, not
// only the Deploys table.
test.describe('UU4: running phase in the Stacks roster', () => {
  test.use({
    startOptions: { stacks: ['web'], discovery: { repoConfig: WEB_HELD }, readiness: 'listening' },
  });

  test('the roster row shows the same phase while a hook runs', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);
    await page.locator('[data-testid="view-toggle"] button[data-view="stacks"]').click();

    const rrow = page.locator('[data-testid="roster-row"][data-stack="web"]');
    await expect(rrow.locator('[data-testid="hook-phase"]')).toContainText('pre_deploy hook');
    await expect(rrow.locator('[data-testid="hooks-badge"]')).toHaveAttribute('data-hook-active', '1');

    skipper.releaseHook(); // let the held hook finish so the deploy settles and teardown is clean
  });
});
