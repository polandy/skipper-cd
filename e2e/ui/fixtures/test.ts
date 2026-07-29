import { test as base, type Page } from '@playwright/test';
import { Skipper, type StartOptions } from './harness';

// The `skipper` fixture starts a fresh skipper binary for each test (its own
// origin, stub docker, and free ports) and tears it down afterwards. On failure
// the process output is attached to the report for debugging, together with the
// UI's own diagnostics (window.__uiNotes).
//
// Both are read *after* the test has finished, never subscribed to while it
// runs: attaching a console or network listener is itself enough to change the
// timing of a race, which is how the UC11 investigation kept losing its own
// flake (T8).
//
// Opt into stub-docker scripting per test with `test.use({ startOptions: … })`.
export const test = base.extend<{
  skipper: Skipper;
  startOptions: StartOptions;
  seedTourSeen: boolean;
}>({
  startOptions: [{ stacks: ['web'] }, { option: true }],

  // Every test runs as a returning browser by default: the first-run header tour
  // (T3.15) is pre-seeded as seen, so the steady-state header (not the one-time
  // captions/banner) is what visual baselines and unrelated behaviour tests see.
  // The tour's own spec opts out with `test.use({ seedTourSeen: false })`.
  seedTourSeen: [true, { option: true }],

  page: async ({ page, seedTourSeen }, use) => {
    if (seedTourSeen) {
      await page.addInitScript(() => localStorage.setItem('headerTourSeen', '1'));
    }
    await use(page);
  },

  skipper: async ({ startOptions, page }, use, testInfo) => {
    const s = await Skipper.start(startOptions);
    await use(s);
    if (testInfo.status !== testInfo.expectedStatus) {
      await testInfo.attach('skipper-output', { body: s.output(), contentType: 'text/plain' });
      await attachUINotes(page, testInfo);
    }
    await s.stop();
  },
});

// attachUINotes copies the page's own diagnostics into the report. Best-effort:
// a test can fail with the page already closed or navigated away, and losing the
// notes must never turn a failure into a confusing second one.
async function attachUINotes(
  page: Page,
  testInfo: { attach: (name: string, o: { body: string; contentType: string }) => Promise<void> },
): Promise<void> {
  try {
    const notes = await page.evaluate(() => (window as { __uiNotes?: string[] }).__uiNotes ?? []);
    await testInfo.attach('ui-notes', {
      body: notes.length ? notes.join('\n') : '(the UI recorded no diagnostics)',
      contentType: 'text/plain',
    });
  } catch {
    // page gone — nothing to collect.
  }
}

export { expect } from '@playwright/test';
