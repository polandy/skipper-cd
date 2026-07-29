import { test, expect, type Page } from '../fixtures/test';

// Maske AN: the web-font swap causes no layout shift. See
// dev-docs/e2e-tests.md §4.41.
//
// app.css declares metric-compatible local fallback faces (size-adjust +
// ascent/descent overrides) for DM Sans and JetBrains Mono, so text rendered
// before the self-hosted webfonts arrive occupies exactly the space of the
// loaded faces. The regression this pins: the queue empty-note used to render
// one line in the container's Ubuntu Mono (0.5em advance) and wrap to two when
// JetBrains Mono (0.6em) swapped in, dropping everything below it ~19px — a
// real CLS on slow connections and the root of the UC11 flake. The spec
// renders the autosync drawer twice — webfonts blocked, then loaded — and
// asserts identical geometry.
const autosyncBtn = (page: Page) => page.locator('[data-testid="autosync-btn"]');
const autosyncDrawer = (page: Page) => page.locator('[data-testid="autosync-drawer"]');

async function openDrawer(page: Page) {
  await autosyncBtn(page).click();
  await expect(autosyncDrawer(page)).toHaveAttribute('data-ready', 'true');
  // See uc-autosync.spec.ts: wait out the open transition's moving geometry.
  await expect(autosyncDrawer(page)).toHaveAttribute('data-settled', 'true');
}

/** The geometry the historical hop moved: the queue empty-note's box and the
 *  vertical position of the first stack switch below it. */
async function drawerGeometry(page: Page) {
  const note = autosyncDrawer(page).locator('.qempty').first();
  await expect(note).toBeVisible();
  const noteBox = await note.boundingBox();
  const switchBox = await page
    .locator('[data-testid="stack-switch"]')
    .first()
    .boundingBox();
  expect(noteBox).not.toBeNull();
  expect(switchBox).not.toBeNull();
  return { noteBox: noteBox!, switchY: switchBox!.y };
}

test.describe('UAN1: font-swap layout stability', () => {
  test('the autosync drawer lays out identically with webfonts blocked and loaded', async ({
    page,
    skipper,
  }) => {
    // Pass 1 — the slow-connection render: abort every webfont request so the
    // page settles on the metric-adjusted local fallbacks.
    await page.route('**/fonts/**', (route) => route.abort());
    await page.goto(`${skipper.baseURL}/`);
    // Positive signal that this pass measured the fallback render: the
    // webfont must NOT be available (guards against a future caching layer
    // silently serving it despite the abort, which would turn the comparison
    // below into loaded-vs-loaded and let a fallback regression pass green).
    expect(await page.evaluate(() => document.fonts.check('12.5px "JetBrains Mono"'))).toBe(false);
    await openDrawer(page);
    const fallback = await drawerGeometry(page);

    // Pass 2 — the loaded render (the fixture's goto/reload already await
    // document.fonts.ready, so the webfonts are in before we measure).
    await page.unroute('**/fonts/**');
    await page.reload();
    expect(await page.evaluate(() => document.fonts.check('12.5px "JetBrains Mono"'))).toBe(true);
    await openDrawer(page);
    const loaded = await drawerGeometry(page);

    // The empty note occupies the same box and the switches sit at the same
    // height — the swap moved nothing. 1px of slack absorbs subpixel
    // rasterisation; the regression this guards was a ~19px hop.
    expect(Math.abs(fallback.noteBox.height - loaded.noteBox.height)).toBeLessThanOrEqual(1);
    expect(Math.abs(fallback.noteBox.width - loaded.noteBox.width)).toBeLessThanOrEqual(1);
    expect(Math.abs(fallback.noteBox.y - loaded.noteBox.y)).toBeLessThanOrEqual(1);
    expect(Math.abs(fallback.switchY - loaded.switchY)).toBeLessThanOrEqual(1);
  });
});
