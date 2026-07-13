import { expect, type Locator, type Page } from '@playwright/test';

// Visual snapshots are pixel-deterministic only inside Playwright's pinned
// container (its Chromium + fonts fix the rasterisation). A local run on the
// host — where PW_CHROMIUM_EXECUTABLE points at a system Chromium — would
// rasterise differently, so the pixel compare is opt-in via RUN_SNAPSHOTS,
// which the `e2e-ui` CI job (and the baseline-generation run) sets. When it is
// unset the behaviour assertions still run; only the screenshot is skipped, so
// `docs/e2e-tests.md` §5 holds: local runs compare behaviour, CI compares pixels.
export const snapshotsEnabled = !!process.env.RUN_SNAPSHOTS;

type ScreenshotOptions = Parameters<ReturnType<typeof expect<Locator>>['toHaveScreenshot']>[1];

/**
 * visualSnapshot compares `target` against the named baseline, but only when
 * snapshots are enabled (RUN_SNAPSHOTS set / the pinned container). Elsewhere it
 * is a no-op, leaving the surrounding behaviour test intact.
 */
export async function visualSnapshot(
  target: Page | Locator,
  name: string,
  options?: ScreenshotOptions,
): Promise<void> {
  if (!snapshotsEnabled) return;
  await expect(target).toHaveScreenshot(name, options);
}
