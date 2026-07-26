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

// UP2 — the tag must not cost the row its identity. `.stack-name` carries
// overflow:hidden, which resolves its automatic flex minimum to zero, so before
// the min-width floor it was the *only* item in the cell that could shrink while
// the nowrap tag refused to give way at all — a long dependency name clipped the
// stack name past its own ellipsis. The name now stays whole at every width; the
// tag ellipsises and is dropped below 1000px, where it had nothing useful left
// to say (the badge still reads BLOCKED, the reason stays in the drawer).
test.describe('UP2: the pending tag yields before the stack name', () => {
  test.use({
    startOptions: {
      stacks: [DEP, 'app'],
      dependsOn: { app: [DEP] },
      stubEnv: { STUB_DOCKER_FAIL_NTH_UP: '3' },
    },
  });

  test('the name stays whole while the tag gives way', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);

    // Same trigger as UP1: change both stacks so the dependency's `up` fails
    // and `app` is held back. Locating by data-status waits for that row rather
    // than picking whichever row is newest at the time.
    skipper.setStackImage(DEP, '1.26');
    skipper.setStackImage('app', '1.26');
    expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);

    const blocked = page.locator('[data-testid="deploy-row"][data-stack="app"][data-status="blocked"]');
    await expect(blocked).toHaveCount(1);

    const name = blocked.locator('.stack-name');
    const tag = blocked.locator('.paused-tag');
    const clipped = () =>
      name.evaluate((el) => el.scrollWidth > Math.ceil(el.getBoundingClientRect().width));

    // Wide: both fit.
    await page.setViewportSize({ width: 1400, height: 900 });
    await expect(tag).toBeVisible();
    expect(await clipped()).toBe(false);

    // Narrow enough that something has to give — the tag does, not the name.
    await page.setViewportSize({ width: 1100, height: 900 });
    expect(await clipped()).toBe(false);

    // Below the 1000px breakpoint the tag is gone rather than a one-letter stub,
    // and the name is still whole.
    await page.setViewportSize({ width: 900, height: 900 });
    await expect(tag).toBeHidden();
    expect(await clipped()).toBe(false);
    await expect(name).toHaveText('app');
  });
});
