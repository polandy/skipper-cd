import { test, expect } from "../fixtures/test";
import type { Page } from "@playwright/test";

// Maske AV: the Stacks view's updates filter (ADR-0054 amendment). See
// dev-docs/e2e-tests.md §4.49.
//
// The per-chip `⇡` marker (Maske AO) answers "does THIS stack have an update".
// This mask covers the fleet-level surface built on top of it: an always-visible
// header badge counting the STACKS an update is waiting on, and the single
// updates-only toggle in the Stacks filter bar it presets and clears.
//
// The fixture deliberately includes a stack whose update sits on a NON-lead
// service (`monitoring` over prometheus/grafana, with grafana updating). That is
// the case that made the badge un-countable before the amendment: the row showed
// only `2 services`, so the badge claimed three stacks while two markers were on
// screen. UAV4 is the regression guard for exactly that.

const UPSTREAM_DIGEST =
  "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";

test.use({
  startOptions: {
    stacks: ["gitea", "monitoring", "plain"],
    healthPoll: 1,
    initialCompose: {
      gitea:
        "services:\n  server:\n    image: {{registry}}/gitea/gitea:1.22.3\n",
      monitoring:
        "services:\n  prometheus:\n    image: {{registry}}/prom/prometheus:v3.1.0\n" +
        "  grafana:\n    image: {{registry}}/grafana/grafana:11.6.0\n",
      plain: "services:\n  app:\n    image: {{registry}}/plain:1.0.0\n",
    },
    initialHealth: {
      gitea: [
        {
          Service: "server",
          Name: "gitea-server-1",
          Image: "{{registry}}/gitea/gitea:1.22.3",
          State: "running",
          Health: "healthy",
        },
      ],
      monitoring: [
        {
          Service: "prometheus",
          Name: "monitoring-prometheus-1",
          Image: "{{registry}}/prom/prometheus:v3.1.0",
          State: "running",
          Health: "healthy",
        },
        {
          Service: "grafana",
          Name: "monitoring-grafana-1",
          Image: "{{registry}}/grafana/grafana:11.6.0",
          State: "running",
          Health: "healthy",
        },
      ],
      plain: [
        {
          Service: "app",
          Name: "plain-app-1",
          Image: "{{registry}}/plain:1.0.0",
          State: "running",
          Health: "healthy",
        },
      ],
    },
    updateCheck: {
      tags: {
        // The lead service updates — the row chip stays on it.
        "gitea/gitea": ["1.21.0", "1.22.3", "1.22.6", "latest"],
        // A NON-lead service updates: `monitoring` names neither of its
        // services, so without the amendment its row shows no marker at all.
        "grafana/grafana": ["11.6.0", "12.0.1"],
        "prom/prometheus": ["v3.1.0"],
        // Current: the control stack the filter must hide.
        plain: ["1.0.0"],
      },
      digest: UPSTREAM_DIGEST,
      repoDigests: {
        "{{registry}}/prom/prometheus:v3.1.0": [
          `{{registry}}/prom/prometheus@${UPSTREAM_DIGEST}`,
        ],
        "{{registry}}/plain:1.0.0": [`{{registry}}/plain@${UPSTREAM_DIGEST}`],
      },
    },
  },
});

const badge = (page: Page) => page.locator('[data-testid="update-badge"]');
const chip = (page: Page) => page.locator('[data-testid="roster-update-chip"]');
const filterWrap = (page: Page) =>
  page.locator('[data-testid="roster-filter-wrap"]');
const rows = (page: Page) =>
  page.locator('[data-testid="roster-row"]:not(.filtered-out)');
const row = (page: Page, stack: string) =>
  page.locator(`[data-testid="roster-row"][data-stack="${stack}"]`);

// The check runs after the startup deploys, so the badge is an SSE republish
// away. Waiting for it to carry its final count is the positive signal every
// case below rests on — an assertion made before it lands would pass against a
// UI that never counted anything.
async function openWithBadge(page: Page, baseURL: string): Promise<void> {
  await page.goto(`${baseURL}/`);
  await expect(badge(page)).toBeVisible();
  await expect(page.locator('[data-testid="update-badge-count"]')).toHaveText(
    "2",
  );
}

// UAV1 — the badge counts STACKS, not services, and says so in words.
test("UAV1: the header badge counts the stacks an update is waiting on", async ({
  page,
  skipper,
}) => {
  await openWithBadge(page, skipper.baseURL);
  // monitoring contributes ONE, though only one of its two services updates;
  // gitea contributes one; plain contributes none.
  await expect(badge(page)).toHaveAttribute(
    "aria-label",
    "2 stacks have an update available",
  );
  await expect(badge(page)).toHaveAttribute(
    "title",
    "2 stacks have an update available",
  );
});

// UAV2 — activating it lands on exactly the stacks the count promised.
test("UAV2: the badge opens the Stacks view filtered to the stacks with updates", async ({
  page,
  skipper,
}) => {
  await openWithBadge(page, skipper.baseURL);
  await badge(page).click();
  await expect(page.locator('[data-testid="stacks-view"]')).toBeVisible();
  await expect(filterWrap(page)).toHaveClass(/revealed/);
  await expect(chip(page)).toHaveAttribute("aria-pressed", "true");
  await expect(chip(page).locator(".sc-count")).toHaveText("2");
  await expect(rows(page)).toHaveCount(2);
  await expect(row(page, "gitea")).not.toHaveClass(/filtered-out/);
  await expect(row(page, "monitoring")).not.toHaveClass(/filtered-out/);
  // The control stack is present in the roster and hidden BY the filter — the
  // unfiltered count below is the signal that makes this absence meaningful.
  await expect(row(page, "plain")).toHaveClass(/filtered-out/);
  await expect(page.locator('[data-testid="roster-filter-count"]')).toHaveText(
    "2/3",
  );
});

// UAV3 — the way out is the same control as the way in.
test("UAV3: a second activation clears the filter and folds the bar away", async ({
  page,
  skipper,
}) => {
  await openWithBadge(page, skipper.baseURL);
  await badge(page).click();
  await expect(rows(page)).toHaveCount(2);
  await badge(page).click();
  await expect(rows(page)).toHaveCount(3); // every stack is back
  await expect(filterWrap(page)).not.toHaveClass(/revealed/);
  await expect(page.locator('[data-testid="roster-filter-count"]')).toHaveText(
    "",
  );
});

// UAV4 — the count must be countable off the list it points at. Before the
// ADR-0054 amendment `monitoring` showed `2 services` and no marker, so the
// badge claimed a stack the eye could not find.
test("UAV4: every row the filter leaves standing carries a visible update marker", async ({
  page,
  skipper,
}) => {
  await openWithBadge(page, skipper.baseURL);
  await badge(page).click();
  await expect(rows(page)).toHaveCount(2);
  await expect(
    rows(page).locator('[data-testid="roster-version"] .td-upd'),
  ).toHaveCount(2);
  // The lead service keeps the cell when the lead is the one updating…
  const gitea = row(page, "gitea").locator('[data-testid="roster-version"]');
  await expect(gitea.locator(".td-svc")).toHaveText("server");
  await expect(gitea.locator(".td-upd")).toHaveText("⇡1.22.6");
  // …and the cell steps off the lead when the update sits elsewhere. The stack
  // names neither service, so without the amendment this cell reads "2 services".
  const mon = row(page, "monitoring").locator('[data-testid="roster-version"]');
  await expect(mon.locator(".td-svc")).toHaveText("grafana");
  await expect(mon.locator(".td-upd")).toHaveText("⇡12.0.1");
  await expect(mon.locator(".ver-count")).toHaveText("+1");
});

// UAV5 — the toggle and the name query narrow independently, and Esc clears
// both before it folds.
test("UAV5: the toggle combines with the name query, and Esc clears both", async ({
  page,
  skipper,
}) => {
  await openWithBadge(page, skipper.baseURL);
  await badge(page).click();
  await expect(rows(page)).toHaveCount(2);

  const input = page.locator('[data-testid="roster-filter"]');
  await input.fill("mon");
  await expect(rows(page)).toHaveCount(1);
  await expect(row(page, "monitoring")).not.toHaveClass(/filtered-out/);
  await expect(page.locator('[data-testid="roster-filter-count"]')).toHaveText(
    "1/3",
  );

  // A query no updated stack matches empties the list, and the note names the
  // combination rather than blaming the query alone.
  await input.fill("plain");
  await expect(rows(page)).toHaveCount(0);
  await expect(page.locator('[data-testid="roster-filter-empty"]')).toHaveText(
    "Nothing matches the active filters.",
  );

  await input.press("Escape"); // first press clears query AND toggle
  await expect(rows(page)).toHaveCount(3);
  await expect(chip(page)).toHaveAttribute("aria-pressed", "false");
  await expect(filterWrap(page)).toHaveClass(/revealed/);
  await input.press("Escape"); // second press folds the bar away
  await expect(filterWrap(page)).not.toHaveClass(/revealed/);
});

// UAV6 — the chip stands on its own: revealed by the search trigger, toggled by
// hand, with the badge tracking the same state.
test("UAV6: the chip works from the search trigger, and the badge follows its state", async ({
  page,
  skipper,
}) => {
  await openWithBadge(page, skipper.baseURL);
  await page
    .locator('[data-testid="view-toggle"] button[data-view="stacks"]')
    .click();
  await expect(page.locator('[data-testid="stacks-view"]')).toBeVisible();
  await page.locator('[data-testid="stack-search-btn"]').click();
  await expect(filterWrap(page)).toHaveClass(/revealed/);
  await expect(chip(page)).toHaveAttribute("aria-pressed", "false");
  await expect(rows(page)).toHaveCount(3); // nothing narrowed yet

  await chip(page).click();
  await expect(rows(page)).toHaveCount(2);
  await expect(badge(page)).toHaveClass(/active/); // its own preset now stands
  await chip(page).click();
  await expect(rows(page)).toHaveCount(3);
  await expect(badge(page)).not.toHaveClass(/active/);
});
