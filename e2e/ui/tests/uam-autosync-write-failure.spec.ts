import { test, expect, type Page } from '../fixtures/test';

// Maske AM: a refused autosync write says so. See dev-docs/e2e-tests.md §4.40.
//
// A failed toggle is invisible in the interface — the switch simply does not
// move, which looks exactly like a click that never landed. That ambiguity cost
// real time chasing a UC11 flake, so the failure is now announced on the console
// (the same treatment a dropped stale snapshot gets) and this pins both paths:
// a refusing server, and a request that never arrives.
const autosyncBtn = (page: Page) => page.locator('[data-testid="autosync-btn"]');
const autosyncDrawer = (page: Page) => page.locator('[data-testid="autosync-drawer"]');
const stackSwitch = (page: Page, name: string) =>
  page.locator(`[data-testid="stack-switch"][data-stack="${name}"]`);

async function openDrawer(page: Page) {
  await autosyncBtn(page).click();
  await expect(autosyncDrawer(page)).toHaveAttribute('data-ready', 'true');
  // See uc-autosync.spec.ts: wait out the open transition's moving geometry.
  await expect(autosyncDrawer(page)).toHaveAttribute('data-settled', 'true');
}

test.describe('UAM: a refused autosync write is announced', () => {
  test('a 5xx leaves the switch on server state and says why', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);
    await openDrawer(page);

    const web = stackSwitch(page, 'web');
    await expect(web).toHaveAttribute('aria-checked', 'true');

    await page.route('**/api/autosync', (route) =>
      route.request().method() === 'POST'
        ? route.fulfill({ status: 503, body: 'nope' })
        : route.continue(),
    );

    // The announcement is the signal, not a wait: it is emitted in the same step
    // the switch would otherwise have been repainted in, so the DOM assertion
    // after it cannot be early. Without it, "the switch did not move" would be
    // an absence assertion that passes before the response is even handled.
    const refused = page.waitForEvent('console', (m) =>
      m.text().includes('autosync: toggle refused for stack web'),
    );
    await web.click();
    const msg = await refused;
    expect(msg.text()).toContain('503');

    // The write did not happen, so the control keeps showing server state rather
    // than the value the click asked for.
    await expect(web).toHaveAttribute('aria-checked', 'true');

    // The same note is kept on the page, which is what reaches a CI report — the
    // console alone is collected by nothing, so a failure would arrive without
    // the reason the UI already knew. The harness attaches this on failure.
    const notes = (
      await page.evaluate(() => (window as { __uiNotes?: string[] }).__uiNotes ?? [])
    ).join('\n');
    expect(notes).toContain('autosync: toggle refused for stack web');
    // The intent is recorded before the request, so a buffer holding only this
    // line proves the click reached the handler and the request is what went
    // missing — the distinction T8 turns on.
    expect(notes).toContain('autosync: toggling stack web -> false');
  });

  test('a request that never arrives is announced too', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);
    await openDrawer(page);

    const web = stackSwitch(page, 'web');
    await expect(web).toHaveAttribute('aria-checked', 'true');

    await page.route('**/api/autosync', (route) =>
      route.request().method() === 'POST' ? route.abort('connectionfailed') : route.continue(),
    );

    const failed = page.waitForEvent('console', (m) =>
      m.text().includes('autosync: toggle for stack web did not reach the server'),
    );
    await web.click();
    await failed;
    await expect(web).toHaveAttribute('aria-checked', 'true');

    const notes = (
      await page.evaluate(() => (window as { __uiNotes?: string[] }).__uiNotes ?? [])
    ).join('\n');
    expect(notes).toContain('autosync: toggling stack web -> false');
    expect(notes).toContain('did not reach the server');
  });
});
