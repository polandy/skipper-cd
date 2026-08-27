import { test, expect } from "../fixtures/test";
import type { Page } from "@playwright/test";
import { openRowMenu } from "../fixtures/menu";

// Maske AU: a standing failure repeats without flooding the history (ADR-0056).
// See dev-docs/e2e-tests.md §4.48.
//
// Every `up` fails, so each sync produces the identical `failed` outcome — the
// shape that on 2026-08-18 put 199 identical records into one stack's 200-record
// audit window and 91 of 100 slots into the global event ring. The repeats must
// collapse into one row carrying the count, live and after a reload.
//
// One subtlety the fixture depends on: the *startup* failure has no previous
// commit to restore, so it fails with its own message ("no previous commit
// available for rollback") and is deliberately a separate incident. The run
// therefore opens with the first webhook, and ×2 arrives with the second.
// Behaviour-only (no snapshot).

test.use({
  startOptions: {
    stacks: ["web"],
    stubEnv: { STUB_DOCKER_FAIL_ON: "up" },
    readiness: "listening",
  },
});

const failedRow = (p: Page) =>
  p.locator(
    '[data-testid="deploy-row"][data-stack="web"][data-status="failed"]',
  );
const repeatNote = (p: Page) =>
  failedRow(p).locator('[data-testid="repeat-note"]');

// UAU1 — the repeats land on one row as a count, not as more rows. The count is
// the positive signal the "no further rows" assertion is checked against:
// without the collapse it never appears while the row count climbs.
test("UAU1: a repeated failure collapses into one row that counts it", async ({
  page,
  skipper,
}) => {
  await page.goto(`${skipper.baseURL}/`);

  // The startup deploy already failed once — a single occurrence, no marker.
  await expect(failedRow(page)).toHaveCount(1);
  await expect(repeatNote(page)).toHaveCount(0);

  // A different cause is a different incident, so the differently-worded
  // startup failure keeps its own row and this one opens the run.
  expect(await skipper.sendWebhook("refs/heads/main")).toBe(202);
  await expect(failedRow(page)).toHaveCount(2);
  await expect(repeatNote(page)).toHaveCount(0);

  // Each webhook is settled through the count before the next is sent: deploys
  // serialize on one mutex, so firing them in a burst would not produce one run
  // each (Invariant 7). The row count staying at 2 is what the collapse buys.
  expect(await skipper.sendWebhook("refs/heads/main")).toBe(202);
  await expect(repeatNote(page)).toHaveText(/×2/);
  await expect(failedRow(page)).toHaveCount(2);

  expect(await skipper.sendWebhook("refs/heads/main")).toBe(202);
  await expect(repeatNote(page)).toHaveText(/×3/);
  await expect(failedRow(page)).toHaveCount(2);

  // The whole run is one incident, and the marker says how long it has stood.
  await expect(repeatNote(page)).toHaveAttribute(
    "title",
    /3 identical failures, .+→.+/,
  );
});

// UAU2 — the collapse is in the stores, not in the rendering: a reload rebuilds
// from the persisted history and shows the same counted row.
test("UAU2: the collapsed count survives a reload", async ({
  page,
  skipper,
}) => {
  await page.goto(`${skipper.baseURL}/`);
  await expect(failedRow(page)).toHaveCount(1);

  expect(await skipper.sendWebhook("refs/heads/main")).toBe(202);
  await expect(failedRow(page)).toHaveCount(2);
  expect(await skipper.sendWebhook("refs/heads/main")).toBe(202);
  await expect(repeatNote(page)).toHaveText(/×2/);

  await page.reload();

  await expect(failedRow(page)).toHaveCount(2);
  await expect(repeatNote(page)).toHaveText(/×2/);
});

// UAU3 — the same record reads the same way in the stack's durable deploy
// history, which is the store the flood used to empty.
test("UAU3: the deploy-history panel marks the repeat too", async ({
  page,
  skipper,
}) => {
  await page.goto(`${skipper.baseURL}/`);
  await expect(failedRow(page)).toHaveCount(1);
  expect(await skipper.sendWebhook("refs/heads/main")).toBe(202);
  await expect(failedRow(page)).toHaveCount(2);
  expect(await skipper.sendWebhook("refs/heads/main")).toBe(202);
  await expect(repeatNote(page)).toHaveText(/×2/);

  await openRowMenu(failedRow(page).first());
  await failedRow(page).first().locator('[data-testid="history-btn"]').click();

  const panel = page.locator('[data-testid="audit-panel"]');
  await expect(panel).toBeVisible();
  // Two records — the differently-worded startup failure and the run — rather
  // than one row per attempt, and the run's row carries the count.
  await expect(panel.locator('[data-testid="audit-row"]')).toHaveCount(2);
  await expect(panel.locator('[data-testid="repeat-note"]')).toHaveText(/×2/);
});
