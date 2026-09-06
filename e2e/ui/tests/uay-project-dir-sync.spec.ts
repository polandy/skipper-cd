import { test, expect } from "../fixtures/test";
import type { Page } from "@playwright/test";
import { openRowMenu } from "../fixtures/menu";

// Maske AY: the project_directory checkout is fast-forwarded before the stack
// phase, and a refusal is visible (ADR-0060). See dev-docs/e2e-tests.md §4.52.
//
// Compose serves a stack's relative bind mounts out of --project-directory, not
// out of the clone the compose file came from, so a checkout nobody advances
// mounts stale content behind a green deploy. Skipper fast-forwards it — and
// refuses on a dirty tree, which is exactly the state the operator who edits
// that tree leaves it in. The refusal must be visible, must not block the
// deploys, and must not repeat on every reconcile.
// Behaviour-only (no snapshot).

test.use({
  startOptions: {
    stacks: ["web"],
    projectDirSync: true,
  },
});

const PSEUDO_STACK = "_project_dir";

const phaseRow = (p: Page) =>
  p.locator(`[data-testid="deploy-row"][data-stack="${PSEUDO_STACK}"]`);
const stackRow = (p: Page) =>
  p.locator('[data-testid="deploy-row"][data-stack="web"]');

// UAY1 — a fast-forward that works is plumbing: it leaves no row behind, so the
// history keeps saying only what happened to the stacks. The stack's own
// success row is the positive signal that the run really ran.
test("UAY1: a working fast-forward reports nothing", async ({
  page,
  skipper,
}) => {
  await page.goto(`${skipper.baseURL}/`);

  await expect(stackRow(page)).toHaveCount(1);
  await expect(phaseRow(page)).toHaveCount(0);

  skipper.setStackCompose(
    "web",
    "services:\n  web:\n    image: nginx:1.27\n",
  );
  expect(await skipper.sendWebhook("refs/heads/main")).toBe(202);

  await expect(stackRow(page)).toHaveCount(2);
  await expect(phaseRow(page)).toHaveCount(0);
});

// UAY2 — a dirty checkout stops the fast-forward. The refusal says so as a
// failed row, the stacks deploy anyway, and the checkout is where it was.
test("UAY2: a dirty checkout is reported without blocking the deploys", async ({
  page,
  skipper,
}) => {
  await page.goto(`${skipper.baseURL}/`);
  await expect(stackRow(page)).toHaveCount(1);

  const before = skipper.projectDirHead();
  skipper.dirtyProjectDir();
  skipper.setStackCompose(
    "web",
    "services:\n  web:\n    image: nginx:1.27\n",
  );
  expect(await skipper.sendWebhook("refs/heads/main")).toBe(202);

  const row = phaseRow(page).first();
  await expect(row).toHaveAttribute("data-status", "failed");
  // A stale mount is degraded, not wrong: the stack still converged this run.
  await expect(stackRow(page)).toHaveCount(2);
  expect(skipper.projectDirHead()).toBe(before);

  // The reason is on the row's error panel, naming what to do about it.
  await row.click();
  await expect(page.locator('[data-testid="error-panel"]')).toContainText(
    /uncommitted changes/,
  );
});

// UAY3 — the refusal is a standing condition, not an event: an operator who
// leaves the tree dirty for a week gets one row, not one per reconcile. Row
// count alone would not show this — the history's repeat collapse (ADR-0056)
// folds identical failures into one row anyway — so the assertion is the
// *absence of a repeat marker*: the later runs produced no outcome to fold. The
// climbing stack-row count is the positive signal that they really happened.
test("UAY3: a standing refusal is reported once", async ({ page, skipper }) => {
  await page.goto(`${skipper.baseURL}/`);
  skipper.dirtyProjectDir();

  skipper.setStackCompose(
    "web",
    "services:\n  web:\n    image: nginx:1.27\n",
  );
  expect(await skipper.sendWebhook("refs/heads/main")).toBe(202);
  await expect(phaseRow(page)).toHaveCount(1);
  await expect(stackRow(page)).toHaveCount(2);

  skipper.setStackCompose(
    "web",
    "services:\n  web:\n    image: nginx:1.28\n",
  );
  expect(await skipper.sendWebhook("refs/heads/main")).toBe(202);
  await expect(stackRow(page)).toHaveCount(3);
  await expect(phaseRow(page)).toHaveCount(1);
  await expect(phaseRow(page).locator('[data-testid="repeat-note"]')).toHaveCount(
    0,
  );
});

// UAY4 — the row is a run phase, not a Compose project: no jump to a Stacks
// roster it has no entry in, and no container logs. Same treatment _nixos and
// _config get (isPseudoStack).
test("UAY4: the phase row carries no container-logs or jump affordance", async ({
  page,
  skipper,
}) => {
  await page.goto(`${skipper.baseURL}/`);
  skipper.dirtyProjectDir();
  skipper.setStackCompose(
    "web",
    "services:\n  web:\n    image: nginx:1.27\n",
  );
  expect(await skipper.sendWebhook("refs/heads/main")).toBe(202);

  const row = phaseRow(page).first();
  await expect(row).toHaveAttribute("data-status", "failed");
  await expect(row.locator('[data-testid="jump-btn"]')).toHaveCount(0);

  await openRowMenu(row);
  const menu = row.locator('[data-testid="more-pop"]');
  // The menu still opens and still offers the history — the positive signal
  // that the absent container-logs entry is an omission, not a dead menu.
  await expect(menu.locator('[data-testid="history-btn"]')).toHaveCount(1);
  await expect(menu.locator('[data-testid="clog-btn"]')).toHaveCount(0);

  // The stack's own row keeps both, so the omission is about this row.
  await expect(stackRow(page).first().locator('[data-testid="jump-btn"]')).toHaveCount(1);
});
