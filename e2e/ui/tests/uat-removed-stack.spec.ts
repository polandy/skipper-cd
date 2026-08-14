import { test, expect } from "../fixtures/test";
import type { Page } from "@playwright/test";
import type { OrphanContainer } from "../fixtures/harness";

// Maske AT: a stack removed from the deploy repo (ADR-0036 amendment). See
// dev-docs/e2e-tests.md §4.17.
//
// Boots in discovery mode with two stacks and the health poll on (orphan
// detection rides that cadence). `blog` is then deleted from the repo and
// pushed: the Deploys view must record the removal as its own row against that
// commit, exactly once, while its containers keep running and surface in the
// Orphans section. Behaviour-only (no snapshot).

test.use({
  startOptions: { stacks: ["web", "blog"], discovery: {}, healthPoll: 1 },
});

const removedRow = (p: Page, stack: string) =>
  p.locator(
    `[data-testid="deploy-row"][data-stack="${stack}"][data-status="removed"]`,
  );
const section = (p: Page) => p.locator('[data-testid="orphans"]');
const orphan = (p: Page, project: string) =>
  p.locator(`[data-testid="orphan-item"][data-project="${project}"]`);

// The containers the removed stack leaves running, plus the surviving stack's
// (managed, so it is excluded from the section).
function listingAfterRemoval(base: string): OrphanContainer[] {
  return [
    {
      project: "web",
      workingDir: `${base}/web`,
      name: "web-1",
      service: "app",
      image: "nginx:1.25",
      state: "running",
      status: "Up 4 minutes",
    },
    {
      project: "blog",
      workingDir: `${base}/blog`,
      name: "blog-app-1",
      service: "app",
      image: "nginx:1.25",
      state: "running",
      status: "Up 4 minutes",
    },
  ];
}

// UAT1 — the removal is a row of its own, and the containers it left behind are
// surfaced without a click.
test("UAT1: a removed stack lands in the deploy history and opens the Orphans section", async ({
  page,
  skipper,
}) => {
  await page.goto(`${skipper.baseURL}/`);
  // Nothing is orphaned yet, so the section stays away entirely.
  await expect(section(page)).not.toHaveClass(/\bshown\b/);

  skipper.removeStack("blog");
  skipper.setOrphans(listingAfterRemoval(skipper.stacksBaseDir));
  expect(await skipper.sendWebhook("refs/heads/main")).toBe(202);

  // The row names the stack that left, with the removed badge.
  const row = removedRow(page, "blog");
  await expect(row).toHaveCount(1);
  await expect(row.locator('[data-testid="status-badge"]')).toHaveText(
    "removed",
  );
  await expect(row.locator(".stack-name")).toHaveText("blog");

  // Its containers are still running: the orphan surfaces, and the section opens
  // itself rather than hiding it behind the collapsed header.
  await expect(section(page)).toHaveClass(/\bshown\b/);
  await expect(section(page)).toHaveClass(/\bopen\b/);
  await expect(orphan(page, "blog")).toHaveAttribute("data-class", "orphaned");
  await expect(orphan(page, "web")).toHaveCount(0); // still managed
});

// UAT2 — the removal is announced once. Every later sync re-reads a repo without
// the stack; a row per reconcile tick would bury the history it belongs to.
test("UAT2: the removal is recorded once, not on every later sync", async ({
  page,
  skipper,
}) => {
  await page.goto(`${skipper.baseURL}/`);

  skipper.removeStack("blog");
  expect(await skipper.sendWebhook("refs/heads/main")).toBe(202);
  await expect(removedRow(page, "blog")).toHaveCount(1);

  // A second push that changes the surviving stack. Its new success row is the
  // positive signal that the run happened and re-evaluated the set — without it
  // an unchanged count would prove nothing. Two rows: the startup deploy's and
  // this one's.
  skipper.setStackImage("web", "1.26");
  expect(await skipper.sendWebhook("refs/heads/main")).toBe(202);
  await expect(
    page.locator(
      '[data-testid="deploy-row"][data-stack="web"][data-status="success"]',
    ),
  ).toHaveCount(2);

  await expect(removedRow(page, "blog")).toHaveCount(1);
});
