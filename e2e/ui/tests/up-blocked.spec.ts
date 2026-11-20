import { test, expect } from '../fixtures/test';

// Maske P: blocked rows (ADR-0032) + hostile-name escaping. See
// docs/dev/e2e-tests.md §4.17.
//
// `app` depends on a stack whose name contains markup (`dep<img>x` — legal as a
// directory and stack name). The stub fails the dependency's redeploy (up #3:
// startup deploys are #1/#2), so the run blocks `app` with the reason
// `blocked by dep<img>x`. The UI must render that reason — and the stack name
// itself — as literal text, never as markup: in stack-discovery mode
// (ADR-0034) stack names come from the deploy repo, so a name must not be able
// to inject elements into the page. Behaviour-only (no snapshot).

const DEP = 'dep<img>x';

test.describe('UP1: blocked row renders the dependency reason as text', () => {
  test.use({
    startOptions: {
      stacks: [DEP, 'app'],
      dependsOn: { app: [DEP] },
      stubEnv: { STUB_DOCKER_FAIL_NTH_UP: '3' },
    },
  });

  test('a failed dependency blocks the stack; name and reason stay literal', async ({
    page,
    skipper,
  }) => {
    await page.goto(`${skipper.baseURL}/`);

    // Change both stacks; the dependency deploys first and its `up` fails.
    skipper.setStackImage(DEP, '1.26');
    skipper.setStackImage('app', '1.26');
    expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);

    const blocked = page.locator('[data-testid="deploy-row"][data-stack="app"][data-status="blocked"]');
    await expect(blocked).toHaveCount(1);

    // The cause line names the failed dependency — as text. An unescaped name
    // would inject a real <img> element and swallow the markup from the label.
    const cause = blocked.locator('[data-testid="cause-line"]');
    await expect(cause).toHaveText(`blocked by ${DEP}`);
    await expect(cause).toHaveAttribute('title', `blocked by ${DEP}`);
    await expect(cause.locator('img')).toHaveCount(0);

    // The dependency's own row renders the hostile name literally too.
    const depRow = page.locator(`[data-testid="deploy-row"][data-stack="${DEP}"]`).first();
    await expect(depRow.locator('.stack-name')).toHaveText(DEP);
  });
});

// UP2 — the cause must not cost the row its identity. It sits on its own line
// under the stack name rather than beside it, so the name stays whole at every
// width and the cause ellipsises inside its own line.
test.describe('UP2: the cause line sits under the stack name', () => {
  test.use({
    startOptions: {
      stacks: [DEP, 'app'],
      dependsOn: { app: [DEP] },
      stubEnv: { STUB_DOCKER_FAIL_NTH_UP: '3' },
    },
  });

  test('the name stays whole above its cause at every width', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);

    // Same trigger as UP1: change both stacks so the dependency's `up` fails
    // and `app` is held back. Locating by data-status waits for that row rather
    // than picking whichever row is newest at the time.
    skipper.setStackImage(DEP, '1.26');
    skipper.setStackImage('app', '1.26');
    expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);

    const blocked = page.locator('[data-testid="deploy-row"][data-stack="app"][data-status="blocked"]');
    await expect(blocked).toHaveCount(1);
    // The entrance scales the row, which shrinks the rect clipped() compares against.
    await expect(blocked).not.toHaveClass(/\bnew-row\b/);

    const name = blocked.locator('.stack-name');
    const cause = blocked.locator('[data-testid="cause-line"]');
    const clipped = () =>
      name.evaluate((el) => el.scrollWidth > Math.ceil(el.getBoundingClientRect().width));

    for (const width of [1400, 900, 390]) {
      await page.setViewportSize({ width, height: 900 });
      await expect(cause).toBeVisible();
      expect(await clipped()).toBe(false);
      const n = (await name.boundingBox())!;
      const c = (await cause.boundingBox())!;
      expect(c.y).toBeGreaterThanOrEqual(n.y + n.height - 1);
    }
    await expect(name).toHaveText('app');
  });
});
