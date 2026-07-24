import { test, expect } from '../fixtures/test';
import type { Locator, Page } from '@playwright/test';

// Maske AC: View-toggle active-bar + options caret (T3.12). See dev-docs/e2e-tests.md §4.30.
//
// The active view button carries two distinct cues. A top bar (an intentional
// ::before rectangle) marks it as the active view — on every view, Logs
// included; it replaces the earlier accidental bar, where the a11y sweep's
// touch-target box had reshaped a caret's border into one. A caret (the real
// .vt-caret child) marks that the view has an options popover: it shows only on
// views that have one (deploys/stacks, not Logs) and flips up while open.
// Behaviour + computed-style only (no snapshot).

const viewBtn = (page: Page, view: string) =>
  page.locator(`[data-testid="view-toggle"] button[data-view="${view}"]`);
const options = (page: Page) => page.locator('[data-testid="view-options"]');
const caret = (page: Page, view: string) => viewBtn(page, view).locator('.vt-caret');

// The active-bar is a ::before on the button; read its resolved style.
const bar = (btn: Locator, prop: 'content' | 'height') =>
  btn.evaluate((el, p) => getComputedStyle(el, '::before')[p as never] as string, prop);
const opacity = (el: Locator) =>
  el.evaluate((n) => parseFloat(getComputedStyle(n).opacity));
const transform = (el: Locator) =>
  el.evaluate((n) => getComputedStyle(n).transform);

// UAC1 — The active view button shows both cues; inactive buttons show neither.
// On default deploys: the active-bar (::before, 3px) is drawn and the caret is
// visible; the inactive stacks button has no bar and a hidden caret.
test('UAC1: the active view shows the bar and the caret; inactive views show neither', async ({
  page,
  skipper,
}) => {
  await page.goto(`${skipper.baseURL}/`);

  // Active deploys: bar present, caret visible.
  expect(await bar(viewBtn(page, 'deploys'), 'content')).not.toBe('none');
  expect(await bar(viewBtn(page, 'deploys'), 'height')).toBe('3px');
  expect(await opacity(caret(page, 'deploys'))).toBeGreaterThan(0.5);

  // Inactive stacks: no bar, caret hidden.
  expect(await bar(viewBtn(page, 'stacks'), 'content')).toBe('none');
  expect(await opacity(caret(page, 'stacks'))).toBe(0);
});

// UAC2 — The bar marks "active view" on Logs too, but the options caret does
// not (Logs has no popover — the false-affordance fix). Stacks, which does have
// a popover, shows both — so the caret gate is Logs-specific, not "non-deploys".
test('UAC2: Logs shows the bar but no caret; Stacks shows both', async ({ page, skipper }) => {
  await page.goto(`${skipper.baseURL}/`);

  await viewBtn(page, 'logs').click();
  await expect(viewBtn(page, 'logs')).toHaveClass(/\bactive\b/);
  // Active view → bar present; no popover → no caret.
  expect(await bar(viewBtn(page, 'logs'), 'content')).not.toBe('none');
  // Logs carries no .vt-caret element at all.
  await expect(caret(page, 'logs')).toHaveCount(0);

  await viewBtn(page, 'stacks').click();
  await expect(viewBtn(page, 'stacks')).toHaveClass(/\bactive\b/);
  expect(await bar(viewBtn(page, 'stacks'), 'content')).not.toBe('none');
  expect(await opacity(caret(page, 'stacks'))).toBeGreaterThan(0.5);
});

// UAC3 — The caret flips up while the popover is open and back down when it
// closes. The rotation is CSS-keyed off the active button's aria-expanded; await
// that attribute (deterministic) before reading the settled transform. The flip
// transition is disabled up front so the read never lands mid-tween.
test('UAC3: the caret flips while the popover is open', async ({ page, skipper }) => {
  await page.goto(`${skipper.baseURL}/`);
  const deploys = viewBtn(page, 'deploys');

  await page.addStyleTag({ content: '.vt-caret { transition: none !important; }' });

  // Closed: no rotation (identity 2x2 → matrix(1, …)).
  expect(await transform(caret(page, 'deploys'))).toMatch(/^matrix\(1,/);

  await deploys.click();
  await expect(options(page)).toBeVisible();
  await expect(deploys).toHaveAttribute('aria-expanded', 'true');
  // Flipped 180° (matrix(-1, …)).
  expect(await transform(caret(page, 'deploys'))).toMatch(/^matrix\(-1,/);

  await deploys.click();
  await expect(options(page)).toBeHidden();
  await expect(deploys).toHaveAttribute('aria-expanded', 'false');
  expect(await transform(caret(page, 'deploys'))).toMatch(/^matrix\(1,/);
});

// UAC4 — aria-expanded stays honest across a view switch, so the caret never
// reads "open" on a button that isn't the active one. Open the popover on
// deploys, then switch to stacks: the popover closes and both buttons report
// aria-expanded="false" (deploys must not keep a stale "true").
test('UAC4: switching views leaves no button stuck aria-expanded', async ({ page, skipper }) => {
  await page.goto(`${skipper.baseURL}/`);
  const deploys = viewBtn(page, 'deploys');
  const stacks = viewBtn(page, 'stacks');

  await deploys.click(); // open on deploys
  await expect(deploys).toHaveAttribute('aria-expanded', 'true');

  await stacks.click(); // switch deploys → stacks
  await expect(options(page)).toBeHidden();
  await expect(deploys).toHaveAttribute('aria-expanded', 'false');
  await expect(stacks).toHaveAttribute('aria-expanded', 'false');
});
