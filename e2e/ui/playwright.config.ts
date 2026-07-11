import { defineConfig, devices } from '@playwright/test';

// Playwright config for the skipper-cd UI E2E suite. Each test starts its own
// real skipper binary (see fixtures/harness.ts) against a local git origin and
// a stub docker, so there is no shared server: baseURL is per-test and comes
// from the `skipper` fixture, not from here.
export default defineConfig({
  testDir: './tests',
  // Build the skipper binary once before any test runs.
  globalSetup: './global-setup.ts',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  reporter: [['list'], ['html', { open: 'never' }]],
  use: {
    trace: 'on-first-retry',
  },
  expect: {
    timeout: 10_000,
    // Visual snapshots (later masks) render deterministically with animations
    // disabled and dynamic regions masked; set once here for all screenshots.
    toHaveScreenshot: { animations: 'disabled' },
  },
  projects: [
    {
      name: 'chromium',
      use: {
        ...devices['Desktop Chrome'],
        // Local escape hatch: on hosts where Playwright's bundled Chromium
        // cannot run (e.g. NixOS), point at a system Chromium. Unset in CI,
        // which uses the pinned browser from Playwright's container.
        ...(process.env.PW_CHROMIUM_EXECUTABLE
          ? { launchOptions: { executablePath: process.env.PW_CHROMIUM_EXECUTABLE } }
          : {}),
      },
    },
  ],
});
