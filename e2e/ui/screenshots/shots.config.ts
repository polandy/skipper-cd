import { defineConfig, devices } from '@playwright/test';

// Playwright config for the DOCS SCREENSHOT RENDERER (screenshots/*.spec.ts) —
// separate from the e2e suite's config, whose `testDir: './tests'` deliberately
// excludes this directory: these specs assert nothing about the product, they
// stage a realistic instance and photograph it for the docs site.
//
// The images are generated, never committed (dev-docs/e2e-tests.md §6):
//
//   make docs-screenshots          # local
//   .github/workflows/docs.yml     # CI, before `mkdocs build`
export default defineConfig({
  testDir: '.',
  // One instance at a time: the shots share a single seeded skipper.
  workers: 1,
  // A shot is worth a retry — a flaky render would otherwise publish a
  // half-rendered page or fail the docs build outright.
  retries: 1,
  reporter: [['line']],
  // Staging a full deploy history takes longer than an assertion-sized test.
  timeout: 180_000,
  expect: { timeout: 30_000 },
  projects: [
    {
      name: 'chromium',
      use: {
        ...devices['Desktop Chrome'],
        // 2× for a crisp image on retina displays; the docs site renders these
        // ~1000 px wide, so the extra pixels are for zoom and HiDPI only.
        viewport: { width: 1320, height: 900 },
        deviceScaleFactor: 2,
        // Same local escape hatch as the e2e config (NixOS has no bundled
        // Chromium); unset in CI, which runs Playwright's own container.
        ...(process.env.PW_CHROMIUM_EXECUTABLE
          ? { launchOptions: { executablePath: process.env.PW_CHROMIUM_EXECUTABLE } }
          : {}),
      },
    },
  ],
});
