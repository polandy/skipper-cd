import { defineConfig, devices } from '@playwright/test';

// Playwright config for the skipper-cd UI E2E suite. Each test starts its own
// real skipper binary (see fixtures/harness.ts) against a local git origin and
// a stub docker, so there is no shared server: baseURL is per-test and comes
// from the `skipper` fixture, not from here.
export default defineConfig({
  testDir: './tests',
  // Build the skipper binary once before any test runs.
  globalSetup: './global-setup.ts',
  // Visual-snapshot baselines live under e2e/ui/__screenshots__/ (reviewed like
  // code — GitHub renders them in the diff — but Git LFS-tracked, ADR-0052; see
  // dev-docs/e2e-tests.md §5). Generation and comparison happen only in the
  // pinned container, so the linux/chromium platform never varies — the path is
  // kept flat (no platform suffix).
  snapshotPathTemplate: '__screenshots__/{testFileName}/{arg}{ext}',
  fullyParallel: true,
  // Half the cores in CI (Playwright's own default, made explicit). Each worker
  // drives an isolated skipper binary AND a Chromium — two CPU-hungry processes
  // per worker — so '100%' oversubscribes a 4-core runner and starves the
  // SSE/timer round-trips the timing-sensitive tests wait on, the amplifier
  // behind their intermittent 10s-timeout flakes. '50%' keeps them isolated with
  // headroom. Local runs keep the default so a dev's machine stays responsive.
  workers: process.env.CI ? '50%' : undefined,
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
