import { test, expect } from '../fixtures/test';
import { manifestVersion } from '../fixtures/harness';

// Maske D: Global chrome. See docs/e2e-tests.md §4.5.

// UD5 — Version label. The header shows the deployed skipper-cd version as
// `v<semver>`. globalSetup builds the binary with the version injected via
// -ldflags from .release-please-manifest.json (the same source the Docker/Nix
// builds use), so this asserts the full through-line: ldflags → /api/version →
// header render, against the exact version that ships.
test('UD5: header shows the deployed version', async ({ page, skipper }) => {
  await page.goto(`${skipper.baseURL}/`);

  const label = page.locator('[data-testid="brand-version"]');
  await expect(label).toHaveText(`v${manifestVersion()}`);
});
