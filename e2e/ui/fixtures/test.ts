import { test as base } from '@playwright/test';
import { Skipper, type StartOptions } from './harness';

// The `skipper` fixture starts a fresh skipper binary for each test (its own
// origin, stub docker, and free ports) and tears it down afterwards. On failure
// the process output is attached to the report for debugging.
//
// Opt into stub-docker scripting per test with `test.use({ startOptions: … })`.
export const test = base.extend<{ skipper: Skipper; startOptions: StartOptions }>({
  startOptions: [{ stacks: ['web'] }, { option: true }],

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
