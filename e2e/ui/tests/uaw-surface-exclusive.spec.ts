import { test, expect } from "../fixtures/test";
import type { Page } from "@playwright/test";

// Maske AW: one open header pop-out at a time. See dev-docs/e2e-tests.md §4.50.
//
// The drawers (autosync, hosts, run) and the popovers (view options, health
// beacon) all park under the header at the same anchor, so two open at once is
// never a layout the UI means to show. Sibling to Maske L, which pins the same
// rule one level down, on a deploy row's panels. Behaviour-only (no snapshot).

const autosyncBtn = (page: Page) =>
  page.locator('[data-testid="autosync-btn"]');
const autosyncDrawer = (page: Page) =>
  page.locator('[data-testid="autosync-drawer"]');
const beacon = (page: Page) => page.locator('[data-testid="health-beacon"]');
const beaconPop = (page: Page) =>
  page.locator('[data-testid="health-beacon-pop"]');
const hostsBtn = (page: Page) => page.locator('[data-testid="hosts-btn"]');
const hostsDrawer = (page: Page) =>
  page.locator('[data-testid="hosts-drawer"]');

// UAW1/UAW2 — the health beacon against a drawer, in both directions. The
// beacon's own click stops propagation so that the header click never reaches
// the shared outside-click dismissal, which is what makes this pair the one
// that has to be closed by the surface itself.
test.describe("UAW1: the beacon popover and a drawer never sit open together", () => {
  test.use({
    startOptions: {
      stacks: ["web", "api"],
      healthPoll: 1,
      initialHealth: {
        web: [{ Service: "app", State: "running", Health: "healthy" }],
        api: [{ Service: "app", State: "running", Health: "unhealthy" }],
      },
    },
  });

  test("UAW1: opening the beacon popover closes an open drawer", async ({
    page,
    skipper,
  }) => {
    await page.goto(`${skipper.baseURL}/`);
    await expect(beacon(page)).toBeVisible();

    await autosyncBtn(page).click();
    await expect(autosyncDrawer(page)).toHaveClass(/\bopen\b/);
    await expect(autosyncBtn(page)).toHaveAttribute("aria-expanded", "true");

    await beacon(page).click();

    // The popover the click asked for is showing, and the drawer it replaced
    // has both closed and stopped claiming it is expanded.
    await expect(beaconPop(page)).toBeVisible();
    await expect(beacon(page)).toHaveAttribute("aria-expanded", "true");
    await expect(autosyncDrawer(page)).not.toHaveClass(/\bopen\b/);
    await expect(autosyncBtn(page)).toHaveAttribute("aria-expanded", "false");
  });

  test("UAW2: opening a drawer closes the beacon popover", async ({
    page,
    skipper,
  }) => {
    await page.goto(`${skipper.baseURL}/`);
    await expect(beacon(page)).toBeVisible();

    await beacon(page).click();
    await expect(beaconPop(page)).toBeVisible();

    await autosyncBtn(page).click();

    await expect(autosyncDrawer(page)).toHaveClass(/\bopen\b/);
    await expect(beaconPop(page)).toBeHidden();
    await expect(beacon(page)).toHaveAttribute("aria-expanded", "false");
  });
});

// UAW3 — the two header drawers against each other, both orders. Neither named
// the other before the registry, so this pins the pair that must keep working.
test.describe("UAW3: the hosts and autosync drawers never sit open together", () => {
  const hostB = {
    name: "host-b",
    snapshot: {
      stacks: {
        roster: [
          {
            name: "gitea",
            disabled: false,
            last_status: "success",
            last_at: new Date().toISOString(),
            last_commit: "aaa1111",
          },
        ],
        disabled: [],
      },
      health: { stacks: {} },
      app_links: { stacks: {} },
    },
    audit: [],
  };

  test.use({
    startOptions: {
      stacks: ["api", "web"],
      hostName: "host-a",
      peers: [hostB],
    },
  });

  test("UAW3: whichever drawer opens second replaces the first", async ({
    page,
    skipper,
  }) => {
    await page.goto(`${skipper.baseURL}/`);

    // Hosts first, then autosync.
    await hostsBtn(page).click();
    await expect(hostsDrawer(page)).toHaveClass(/\bopen\b/);
    await autosyncBtn(page).click();
    await expect(autosyncDrawer(page)).toHaveClass(/\bopen\b/);
    await expect(hostsDrawer(page)).not.toHaveClass(/\bopen\b/);
    await expect(hostsBtn(page)).toHaveAttribute("aria-expanded", "false");

    // Autosync first, then hosts.
    await hostsBtn(page).click();
    await expect(hostsDrawer(page)).toHaveClass(/\bopen\b/);
    await expect(autosyncDrawer(page)).not.toHaveClass(/\bopen\b/);
    await expect(autosyncBtn(page)).toHaveAttribute("aria-expanded", "false");
  });
});
