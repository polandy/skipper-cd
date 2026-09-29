import { test, expect } from "../fixtures/test";
import type { Page } from "@playwright/test";

// Maske AZ: a held change and its retry on the Stacks view (ADR-0062).
// See dev-docs/e2e-tests.md §4.53.
//
// A deploy that rolls back (STUB_DOCKER_FAIL_NTH_UP=2: up#1 startup ok, up#2
// fails, up#3 rollback ok) holds its change: the roster row carries the held
// chip and a retry button. The retry deploys the same change once more (up#4
// succeeds), and the next roster snapshot drops the chip.
// Behaviour-only (no snapshot).

const stacksBtn = (page: Page) =>
  page.locator('[data-testid="view-toggle"] button[data-view="stacks"]');
const deployRow = (page: Page, status: string) =>
  page.locator(
    `[data-testid="deploy-row"][data-stack="web"][data-status="${status}"]`,
  );
const rosterRow = (page: Page) =>
  page.locator('[data-testid="roster-row"][data-stack="web"]');
const heldChip = (page: Page) =>
  rosterRow(page).locator('[data-testid="held-chip"]');
const retryBtn = (page: Page) =>
  rosterRow(page).locator('[data-testid="retry-btn"]');

// Drives the rollback and opens the Stacks view on the held row.
async function holdAChange(
  page: Page,
  skipper: import("../fixtures/harness").Skipper,
) {
  await page.goto(`${skipper.baseURL}/`);
  await expect(deployRow(page, "success")).toHaveCount(1); // startup settled
  skipper.setStackImage("web", "1.26");
  expect(await skipper.sendWebhook("refs/heads/main")).toBe(202);
  await expect(deployRow(page, "rolled_back")).toHaveCount(1);
  await stacksBtn(page).click();
  // The hold is published with the run's roster snapshot, after the event.
  await expect(heldChip(page)).toBeVisible();
}

test.describe("Maske AZ: held change", () => {
  test.use({
    startOptions: {
      stacks: ["web"],
      stubEnv: { STUB_DOCKER_FAIL_NTH_UP: "2" },
    },
  });

  // UAZ1 — the chip explains the hold, and retry deploys the change once more:
  // a second success row and the chip gone from the next snapshot.
  test("UAZ1: the held chip names the failure and retry releases it", async ({
    page,
    skipper,
  }) => {
    await holdAChange(page, skipper);

    await expect(heldChip(page)).toHaveText("held");
    await expect(heldChip(page)).toHaveAttribute(
      "title",
      /rolled back .*not retried until a new commit changes web\. Retry deploys it once more\./,
    );
    await expect(retryBtn(page)).toBeEnabled();

    await retryBtn(page).click();

    await expect(heldChip(page)).toHaveCount(0);
    await expect(retryBtn(page)).toHaveCount(0);
    await expect(
      rosterRow(page).locator('[data-testid="status-badge"]'),
    ).toHaveText("success");
    await page
      .locator('[data-testid="view-toggle"] button[data-view="deploys"]')
      .click();
    await expect(deployRow(page, "success")).toHaveCount(2);
  });

  // UAZ2 — a refused retry is announced and hands the button back, so a dead
  // click is never mistaken for a pending one. The announcement is the signal
  // the enabled-again assertion is checked against.
  test("UAZ2: a refused retry re-enables the button and leaves a note", async ({
    page,
    skipper,
  }) => {
    await holdAChange(page, skipper);

    await page.route("**/api/stacks/web/retry", (route) =>
      route.fulfill({
        status: 409,
        body: "stack web has no held change to retry",
      }),
    );
    const refused = page.waitForEvent("console", (m) =>
      m.text().includes("retry: refused for web"),
    );
    await retryBtn(page).click();
    const msg = await refused;
    expect(msg.text()).toContain("409");

    await expect(retryBtn(page)).toBeEnabled();
    await expect(retryBtn(page)).toHaveText("retry");
    await expect(heldChip(page)).toBeVisible();
    const notes = (
      await page.evaluate(
        () => (window as { __uiNotes?: string[] }).__uiNotes ?? [],
      )
    ).join("\n");
    expect(notes).toContain("retry: requesting web");
  });
});
