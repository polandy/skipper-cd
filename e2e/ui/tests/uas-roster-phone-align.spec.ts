import { test, expect } from "../fixtures/test";
import type { Page } from "@playwright/test";

// Maske AS: the rows line up on a phone. See docs/dev/e2e-tests.md §4.46.
//
// The 2026-08-10 report from a phone: the Stacks rows looked untidy. The cause
// was the status column — an `auto` track whose contents were left-aligned, so
// the widest element of each row set that row's width. The last-incident line
// (`↺ rolled back · unhealthy · 8d21h ago`, nowrap) is by far the widest thing
// the cell can hold: it grew the track to over half the row, which squeezed the
// version cell to nothing and cut the version chip off mid-glyph. The mask
// drives a real rollback-then-retry (the sequence that produces an incident
// line) at phone width and asserts the row's geometry. The incident line has
// since moved into the detail panel, so the row is two lines (UI_SPEC Responsive
// — phone): health pill and last deploy share one right edge (UAS1), the
// version cell keeps a real width beside the status on the second line (UAS2),
// a long version chip stays on one line and gives up its label before the
// change (UAS3), the Deploys row gets the same edge, since the two views are
// one look (UAS4), and a live row's
// entrance never replays when the view comes back (UAS5). Behaviour-only, no
// snapshot.

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

type Box = { x: number; y: number; width: number; height: number };

// Rectangles of a roster row and its parts ('' is the row itself), read in one
// page task: a health poll or snapshot rebuilds the row, so parts read one call
// apart can come from two different rows, or from one already detached.
async function rowRects(
  page: Page,
  stack: string,
  parts: string[],
): Promise<Box[]> {
  await expect(rosterRow(page, stack)).toBeVisible();
  return page.evaluate(
    ([rowSel, sels]) => {
      const row = document.querySelector(rowSel);
      if (!row) throw new Error(`no ${rowSel}`);
      return sels.map((sel) => {
        const el = sel ? row.querySelector(sel) : row;
        if (!el) throw new Error(`no ${sel} in ${rowSel}`);
        const r = el.getBoundingClientRect();
        return { x: r.x, y: r.y, width: r.width, height: r.height };
      });
    },
    [`[data-testid="roster-row"][data-stack="${stack}"]`, parts] as const,
  );
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
// successful retry — after which the roster row shows a SUCCESS badge and the
// detail panel the last-incident line naming the rollback.
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
    rosterRow(page, "web").locator('[data-testid="status-badge"]'),
  ).toHaveClass(/badge-success/);
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

  // UAS1 — one right edge for the row's right-hand cells: the health pill on
  // the first line and the last deploy (badge + time) on the second. Left-aligned, they only
  // agreed when one happened to be as wide as the other, which is what made the
  // column read as ragged.
  test("UAS1: health pill and last deploy share the row right edge", async ({
    page,
    skipper,
  }) => {
    await rollBackThenRetry(page, skipper);
    await expect(
      rosterRow(page, "web").locator('[data-testid="health-pill"]'),
    ).toBeVisible();
    const [pill, badge, last] = await rowRects(page, "web", [
      '[data-testid="health-pill"]',
      '[data-testid="status-badge"]',
      ".roster-last",
    ]);

    expect(badge.y).toBeGreaterThanOrEqual(pill.y + pill.height);
    expect(
      Math.abs(pill.x + pill.width - (last.x + last.width)),
    ).toBeLessThanOrEqual(1);
  });

  // UAS2 — the version keeps a real width on the second line, beside the last
  // deploy rather than under it (it was squeezed to 0 when the incident line
  // sized the status column), and the row's SHA gives up its place.
  test("UAS2: the version cell keeps its width beside the last deploy", async ({
    page,
    skipper,
  }) => {
    await rollBackThenRetry(page, skipper);
    const row = rosterRow(page, "web");

    await expect(row.locator(".col-version")).toBeVisible();
    const [version, badge, rowBox] = await rowRects(page, "web", [
      ".col-version",
      '[data-testid="status-badge"]',
      "",
    ]);

    expect(version.width).toBeGreaterThan(60);
    expect(version.x + version.width).toBeLessThanOrEqual(badge.x);
    // Same line: the badge's vertical centre falls inside the version cell.
    const mid = badge.y + badge.height / 2;
    expect(mid).toBeGreaterThan(version.y);
    expect(mid).toBeLessThan(version.y + version.height);
    expect(version.y + version.height).toBeLessThanOrEqual(
      rowBox.y + rowBox.height,
    );
    await expect(row.locator(".roster-sha")).toBeHidden();
  });

  // UAS4 — the Deploys row gets the same edge: the time on the first line and
  // the status cell (badge plus any note) on the second. The two views share one look
  // (docs/dev/ui-design-concept.md), so fixing the roster alone would have left
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
    // The row's entrance scales it: badge and time, read one after the other
    // while it runs, land on different frames (the edge moved by 1.7 px).
    await expect(row).not.toHaveClass(/\bnew-row\b/);
    const status = await rect(row.locator(".status-cell"));
    const time = await rect(row.locator('[data-testid="time-cell"]'));
    expect(status.y).toBeGreaterThanOrEqual(time.y + time.height);
    expect(
      Math.abs(time.x + time.width - (status.x + status.width)),
    ).toBeLessThanOrEqual(1);
  });

  // UAS5 — a live row makes its entrance once. The class that plays it used to
  // stay on, so every return to the Deploys view slid all live rows in again,
  // and a row mid-entrance has no stable geometry to line up (UAS4's flake).
  test("UAS5: returning to the Deploys view does not replay a row's entrance", async ({
    page,
    skipper,
  }) => {
    await rollBackThenRetry(page, skipper);
    await page
      .locator('[data-testid="view-toggle"] button[data-view="deploys"]')
      .click();

    const rows = page.locator('[data-testid="deploy-row"][data-stack="web"]');
    // The rollback and the retry arrive live — the rows that carry an entrance.
    expect(await rows.count()).toBeGreaterThan(1);
    for (const row of await rows.all()) {
      await expect(row).not.toHaveClass(/\bnew-row\b/);
      expect(await row.evaluate((el) => el.getAnimations().length)).toBe(0);
    }
  });
});

// UAS3 — a version chip too long for its cell stays on one line: the service
// label ellipsises, never the change itself. The Deploys column is the narrow
// one, so this drives it there: a long service name plus long tags at phone
// width.
test.describe("Maske AS: a long version chip keeps its change whole", () => {
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

  test("UAS3: the label gives way and the change tokens stay on one line", async ({
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

    // The bump row by what it shows, not by position: the startup row's chip has no old tag.
    const bumpRow = page
      .locator(
        '[data-testid="deploy-row"][data-stack="web"][data-status="success"]',
      )
      .filter({ has: page.locator(".tag-delta .td-old") });
    await expect(bumpRow).toHaveCount(1);
    const chip = bumpRow.locator(".tag-delta");
    await expect(chip).toBeVisible();

    const box = await rect(chip);
    const group = await rect(chip.locator(".td-val"));
    const label = chip.locator(".td-svc");

    // The label really is in the clipped case — otherwise the assertions below
    // would hold trivially and prove nothing.
    expect(
      await label.evaluate((el) => el.scrollWidth > el.clientWidth),
    ).toBe(true);

    // One line, and the change ends inside the chip.
    expect(box.height).toBeLessThan(group.height * 1.5);
    expect(group.x + group.width).toBeLessThanOrEqual(box.x + box.width + 1);
    const tops = await chip
      .locator(".td-val > *")
      .evaluateAll((els) =>
        els.map((e) => Math.round(e.getBoundingClientRect().top)),
      );
    expect(tops.length).toBeGreaterThan(1);
    expect(new Set(tops).size).toBe(1);
  });
});
