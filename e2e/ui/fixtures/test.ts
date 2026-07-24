import { test as base } from '@playwright/test';
import { Skipper, type StartOptions } from './harness';

// The `skipper` fixture starts a fresh skipper binary for each test (its own
// origin, stub docker, and free ports) and tears it down afterwards. On failure
// the process output is attached to the report for debugging.
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

  skipper: async ({ startOptions }, use, testInfo) => {
    const s = await Skipper.start(startOptions);
    await use(s);
    if (testInfo.status !== testInfo.expectedStatus) {
      await testInfo.attach('skipper-output', { body: s.output(), contentType: 'text/plain' });
    }
    await s.stop();
  },
});

export { expect } from '@playwright/test';
