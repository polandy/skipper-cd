import { test, expect } from "../fixtures/test";
import type { Page } from "@playwright/test";

// Maske AS: the rows line up on a phone. See dev-docs/e2e-tests.md §4.46.
//
// The 2026-08-10 report from a phone: the Stacks rows looked untidy. The cause
// was the status column — an `auto` track whose contents were left-aligned, so
// the widest element of each row set that row's width. The last-incident line
// (`↺ rolled back · unhealthy · 8d21h ago`, nowrap) is by far the widest thing
// the cell can hold: it grew the track to over half the row, which squeezed the
// version cell to nothing and cut the version chip off mid-glyph. The mask
// drives a real rollback-then-retry (the sequence that produces an incident
// line) at phone width and asserts the row's geometry: badge, health pill and
// outcome strip share one right edge (UAS1), the incident line has a line of
// its own and no longer costs the version cell its width (UAS2), a wrapping
// version chip never splits a version (UAS3), and the Deploys row gets the same
// edge, since the two views are one look (UAS4). Behaviour-only, no snapshot.

const PHONE = { width: 448, height: 900 }; // Pixel 9 Pro, portrait

const rosterRow = (page: Page, stack: string) =>
  page.locator(`[data-testid="roster-row"][data-stack="${stack}"]`);

// Rectangle of a locator that must be there: waits for it, so a missing
// element fails as "not visible" rather than as a boundingBox timeout.
async function rect(loc: ReturnType<Page["locator"]>) {
  await expect(loc).toBeVisible();
  const box = await loc.boundingBox();
  expect(box).not.toBeNull();
  return box!;
}

async function openStacksView(page: Page): Promise<void> {
  await page
    .locator('[data-testid="view-toggle"] button[data-view="stacks"]')
    .click();
  await expect(
    page.locator('[data-testid="roster-row"]').first(),
  ).toBeVisible();
}

// Drives the sequence that leaves a papered-over incident behind: a startup
// success, a deploy that rolls back (STUB_DOCKER_FAIL_NTH_UP=2), then a
// successful retry — after which the roster row shows a SUCCESS badge *and* the
// last-incident line naming the rollback.
async function rollBackThenRetry(
  page: Page,
  skipper: import("../fixtures/harness").Skipper,
): Promise<void> {
  const deployRow = (status: string) =>
    page.locator(
      `[data-testid="deploy-row"][data-stack="web"][data-status="${status}"]`,
    );
  await page.goto(`${skipper.baseURL}/`);
  await expect(deployRow("success")).toHaveCount(1);
  skipper.setStackImage("web", "1.26");
  expect(await skipper.sendWebhook("refs/heads/main")).toBe(202);
  await expect(deployRow("rolled_back")).toHaveCount(1);
  skipper.setStackImage("web", "1.27");
  expect(await skipper.sendWebhook("refs/heads/main")).toBe(202);
  await expect(deployRow("success")).toHaveCount(2);

  await openStacksView(page);
  await expect(
    rosterRow(page, "web").locator('[data-testid="last-incident"]'),
  ).toBeVisible();
}

test.describe("Maske AS: the Stacks row lines up on a phone", () => {
  test.use({
    viewport: PHONE,
    startOptions: {
      stacks: ["web"],
      stubEnv: { STUB_DOCKER_FAIL_NTH_UP: "2" },
      // The status cell only reads as a column when it is fully lit: health
      // seeds the pill, and the running image gives the row the version chip
      // whose width the incident line used to take.
      healthPoll: 1,
      initialHealth: {
        web: [
          {
            Service: "web",
            Image:
              "ghcr.io/acme/web-application-server:30.0.2-alpine-hardened-build-2026",
            State: "running",
            Health: "healthy",
          },
        ],
      },
    },
  });

  // UAS1 — one right edge for the whole status cell. Left-aligned, the pill and
  // the outcome strip only agreed with the badge when the badge happened to be
  // the widest of the three, which is what made the column read as ragged.
  test("UAS1: badge, health pill and outcome strip share the row right edge", async ({
    page,
    skipper,
  }) => {
    await rollBackThenRetry(page, skipper);
    const row = rosterRow(page, "web");

    const badge = await rect(row.locator('[data-testid="status-badge"]'));
    const pill = await rect(row.locator('[data-testid="health-pill"]'));
    const strip = await rect(row.locator('[data-testid="outcome-strip"]'));

    const edge = badge.x + badge.width;
    expect(Math.abs(pill.x + pill.width - edge)).toBeLessThanOrEqual(1);
    expect(Math.abs(strip.x + strip.width - edge)).toBeLessThanOrEqual(1);
  });

  // UAS2 — the incident line gets a line of its own along the row's bottom edge
  // instead of sizing the status column: it sits below the version line, and the
  // version cell keeps a real width (it was squeezed to 0 before).
  test("UAS2: the incident line has its own line and leaves the version cell its width", async ({
    page,
    skipper,
  }) => {
    await rollBackThenRetry(page, skipper);
    const row = rosterRow(page, "web");

    const incident = await rect(row.locator('[data-testid="last-incident"]'));
    const version = await rect(row.locator(".col-version"));
    const rowBox = await rect(row);

    expect(version.width).toBeGreaterThan(60);
    expect(incident.y).toBeGreaterThanOrEqual(version.y + version.height);
    // Inside its own row — the line is out of flow, so its containment is the
    // thing the row's extra bottom padding buys.
    expect(incident.y + incident.height).toBeLessThanOrEqual(
      rowBox.y + rowBox.height,
    );
  });

  // UAS4 — the Deploys row gets the same edge. The two views share one look
  // (dev-docs/ui-design-concept.md), so fixing the roster alone would have left
  // the sibling view ragged in exactly the way the report was about.
  test("UAS4: the deploy row status cell shares the same right edge", async ({
    page,
    skipper,
  }) => {
    await rollBackThenRetry(page, skipper);
    await page
      .locator('[data-testid="view-toggle"] button[data-view="deploys"]')
      .click();

    const row = page
      .locator(
        '[data-testid="deploy-row"][data-stack="web"][data-status="success"]',
      )
      .first();
    const badge = await rect(row.locator('[data-testid="status-badge"]'));
    const pill = await rect(row.locator('[data-testid="health-pill"]'));
    expect(
      Math.abs(pill.x + pill.width - (badge.x + badge.width)),
    ).toBeLessThanOrEqual(1);
  });
});

// UAS3 — a version chip that has to wrap breaks after the service label, never
// inside the change. The Deploys column is where a chip wraps (the roster's
// stays on one line), so this drives it there: a long service name plus long
// tags at phone width.
test.describe("Maske AS: a wrapping version chip keeps its change on one line", () => {
  const LONG_SERVICE = "web-application-server";
  const LONG_TAG = "30.0.2-alpine-hardened-build";

  test.use({
    viewport: PHONE,
    startOptions: {
      stacks: ["web"],
      initialCompose: {
        web: `services:\n  ${LONG_SERVICE}:\n    image: ghcr.io/acme/${LONG_SERVICE}:${LONG_TAG}\n`,
      },
    },
  });

  test("UAS3: the change tokens stay on one line when the chip wraps", async ({
    page,
    skipper,
  }) => {
    await page.goto(`${skipper.baseURL}/`);
    await expect(
      page.locator(
        '[data-testid="deploy-row"][data-stack="web"][data-status="success"]',
      ),
    ).toHaveCount(1);

    skipper.setStackImage("web", "30.0.3-alpine-hardened-build");
    expect(await skipper.sendWebhook("refs/heads/main")).toBe(202);

    const chip = page
      .locator(
        '[data-testid="deploy-row"][data-stack="web"][data-status="success"]',
      )
      .first()
      .locator(".tag-delta");
    await expect(chip).toBeVisible();

    const box = await rect(chip);
    const group = await rect(chip.locator(".td-val"));
    const label = await rect(chip.locator(".td-svc"));

    // The chip really is in the wrapped case — otherwise the assertion below
    // would hold trivially and prove nothing.
    expect(box.height).toBeGreaterThan(group.height * 1.5);
    expect(label.y + label.height).toBeLessThanOrEqual(group.y + 1);

    // Every token of the change shares the group's single line.
    const tops = await chip
      .locator(".td-val > *")
      .evaluateAll((els) =>
        els.map((e) => Math.round(e.getBoundingClientRect().top)),
      );
    expect(tops.length).toBeGreaterThan(1);
    expect(new Set(tops).size).toBe(1);
  });
});
