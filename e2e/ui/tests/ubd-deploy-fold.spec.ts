import { test, expect } from "../fixtures/test";
import type { Skipper } from "../fixtures/harness";

// Maske BD: day separators and the fold of routine deploys. See
// docs/dev/e2e-tests.md §4.57. Behaviour-only (no snapshot).
//
// Each webhook carries a new image tag, so every deploy is a `success` with an
// image change: the same stack, status and change kind, which is what folds.

test.use({ startOptions: { stacks: ["web"] } });

const webRows = (p: import("@playwright/test").Page) =>
  p.locator('[data-testid="deploy-row"][data-stack="web"][data-status="success"]');
const fold = (p: import("@playwright/test").Page) => p.locator('[data-testid="deploy-fold"]');

async function deployTags(
  page: import("@playwright/test").Page,
  skipper: Skipper,
  tags: string[],
) {
  for (const [i, tag] of tags.entries()) {
    skipper.setStackImage("web", tag);
    expect(await skipper.sendWebhook("refs/heads/main")).toBe(202);
    // Deploys serialize on one mutex: settle each row before the next push.
    await expect(webRows(page)).toHaveCount(i + 2);
  }
}

// UBD1 — a run of routine deploys is one row plus one fold line; a click opens
// the older rows and a second click folds them again.
test("UBD1: six routine deploys fold into the newest row plus a fold line", async ({
  page,
  skipper,
}) => {
  await page.goto(`${skipper.baseURL}/`);
  await expect(webRows(page)).toHaveCount(1);
  await deployTags(page, skipper, ["1.26", "1.27", "1.28", "1.29", "1.30"]);

  await expect(fold(page)).toHaveCount(1);
  await expect(fold(page)).toHaveText(/↻ \d+ more successful deploys of web/);
  await expect(fold(page)).toHaveAttribute("aria-expanded", "false");
  // The newest row stays; every older one is hidden behind the line.
  await expect(webRows(page).first()).toBeVisible();
  await expect(webRows(page).nth(1)).toBeHidden();

  await fold(page).click();
  await expect(fold(page)).toHaveAttribute("aria-expanded", "true");
  await expect(webRows(page).nth(1)).toBeVisible();

  await fold(page).click();
  await expect(fold(page)).toHaveAttribute("aria-expanded", "false");
  await expect(webRows(page).nth(1)).toBeHidden();
});

// UBD2 — the list opens with a day separator in local time.
test("UBD2: rows sit under a Today separator", async ({ page, skipper }) => {
  await page.goto(`${skipper.baseURL}/`);
  await expect(webRows(page)).toHaveCount(1);
  await expect(page.locator('[data-testid="deploy-day"]')).toHaveText(/^(Today|Yesterday)$/);
});

// UBD3 — a filter acts before the fold: with every row filtered out no fold
// line is on screen, and the empty note is the positive signal that the filter ran.
test("UBD3: a name filter that hides the rows takes the fold line with them", async ({
  page,
  skipper,
}) => {
  await page.goto(`${skipper.baseURL}/`);
  await expect(webRows(page)).toHaveCount(1);
  await deployTags(page, skipper, ["1.26", "1.27", "1.28"]);
  await expect(fold(page)).toHaveCount(1);

  await page.locator('[data-testid="stack-search-btn"]').click();
  await page.locator('[data-testid="deploy-filter"]').fill("nomatch");
  await expect(page.locator('[data-testid="deploy-filter-empty"]')).toBeVisible();
  await expect(fold(page)).toHaveCount(0);
});
