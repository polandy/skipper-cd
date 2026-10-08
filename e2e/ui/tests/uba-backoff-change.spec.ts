import { test, expect } from "../fixtures/test";
import type { Page } from "@playwright/test";

// Maske BA: a change that failed before it started backs off, then is held
// (ADR-0064). See dev-docs/e2e-tests.md §4.54.
//
// STUB_DOCKER_FAIL_BUILDS=3 fails the first three builds the way BuildKit
// reports a failed RUN step. The pushed Dockerfile fails its first attempt, so
// the roster row reads "backoff"; each retry click is one more attempt, and
// the third failure turns the chip into "held".
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

test.describe("Maske BA: backoff before a hold", () => {
  test.use({
    startOptions: {
      stacks: ["web"],
      stubEnv: { STUB_DOCKER_FAIL_BUILDS: "3" },
    },
  });

  // UBA1 — the chip says the change still retries on its own and when; each
  // retry is one attempt, and the third failure holds it.
  test("UBA1: backoff names the attempts and the next try, then turns into held", async ({
    page,
    skipper,
  }) => {
    await page.goto(`${skipper.baseURL}/`);
    await expect(deployRow(page, "success")).toHaveCount(1); // startup settled
    skipper.setRepoFile(
      "web/Dockerfile",
      "FROM nginx:1.27\nRUN apt-get install -y ghostscript=0.0-missing\n",
    );
    skipper.setStackCompose(
      "web",
      "services:\n  app:\n    build: .\n    image: web:local\n",
    );
    expect(await skipper.sendWebhook("refs/heads/main")).toBe(202);
    await expect(deployRow(page, "failed")).toHaveCount(1);
    await stacksBtn(page).click();

    await expect(heldChip(page)).toHaveText("backoff");
    await expect(heldChip(page)).toHaveAttribute(
      "title",
      /failed before it started \(1 attempt so far\),.*\. Next attempt .*; if it keeps failing, it is held\. Retry attempts it now\./,
    );
    await expect(retryBtn(page)).toBeEnabled();

    await retryBtn(page).click();
    await expect(heldChip(page)).toHaveAttribute(
      "title",
      /\(2 attempts so far\)/,
    );
    await expect(heldChip(page)).toHaveText("backoff");

    await retryBtn(page).click();
    await expect(heldChip(page)).toHaveText("held");
    await expect(heldChip(page)).toHaveAttribute(
      "title",
      /failed 3 times before it started, last .*not retried until a new commit changes web\. Retry deploys it once more\./,
    );
    await expect(retryBtn(page)).toBeEnabled();
  });
});
