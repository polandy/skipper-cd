import { test, expect } from '../fixtures/test';

// Maske P: blocked rows (ADR-0032) + hostile-name escaping. See
// dev-docs/e2e-tests.md §4.17.
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

    // The tag names the failed dependency — as text. An unescaped name would
    // inject a real <img> element and swallow the markup from the label.
    const tag = blocked.locator('.paused-tag');
    await expect(tag).toHaveText(`blocked by ${DEP}`);
    await expect(tag.locator('img')).toHaveCount(0);

    // The dependency's own row renders the hostile name literally too.
    const depRow = page.locator(`[data-testid="deploy-row"][data-stack="${DEP}"]`).first();
    await expect(depRow.locator('.stack-name')).toHaveText(DEP);
  });
});
