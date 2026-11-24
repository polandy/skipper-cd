import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test, expect } from "../fixtures/test";

// Maske BE: the rolling sub-label of a zero-downtime rollout. See
// docs/dev/e2e-tests.md §4.58. Behaviour-only (no snapshot).
//
// The stub docker answers the rolled service's containers from a table file
// (STUB_DOCKER_ROLLOUT_FILE): a canary that reads `starting` parks the cutover in
// its health wait until the test rewrites the table — no wall-clock race.

const TRAEFIK_COMPOSE = `services:
  app:
    image: nginx:1.25
    labels:
      traefik.enable: "true"
      traefik.http.routers.web.rule: Host(\`web.test\`)
      traefik.http.services.web.loadbalancer.server.port: "80"
    healthcheck:
      test: ["CMD", "true"]
`;

const ROLLOUT_CONFIG = (timeoutSeconds: number) => `stacks:
  web:
    rollout:
      services: [app]
      health_timeout_seconds: ${timeoutSeconds}
`;

const OLD = '{"ID":"old","Service":"app","State":"running","Health":"healthy"}\n';
const canary = (health: string) =>
  `{"ID":"canary","Service":"app","State":"running","Health":"${health}"}\n`;

// One table per case, created at load so test.use can name the path.
const tableDir = mkdtempSync(join(tmpdir(), "skipper-rollout-"));
const heldTable = join(tableDir, "held.jsonl");
const failedTable = join(tableDir, "failed.jsonl");
// A worker that leaves this file and comes back reruns the hooks, not the module.
test.beforeAll(() => mkdirSync(tableDir, { recursive: true }));
test.afterAll(() => rmSync(tableDir, { recursive: true, force: true }));

const phase = (p: import("@playwright/test").Page, scope: string) =>
  p.locator(`${scope}[data-stack="web"] [data-testid="rollout-phase"]`);

// UBE1 — while the canary is not yet healthy the deploying row names the rolled
// service, in the Deploys view and the Stacks roster alike; once the cutover
// ends the label is gone and the deploy succeeds.
test.describe("UBE1: rolling sub-label while a service is cut over", () => {
  test.use({
    startOptions: {
      stacks: ["web"],
      initialCompose: { web: TRAEFIK_COMPOSE },
      discovery: { repoConfig: ROLLOUT_CONFIG(60) },
      stubEnv: { STUB_DOCKER_ROLLOUT_FILE: heldTable, STUB_DOCKER_CANARY_HEALTH: "starting" },
    },
  });

  test("the label shows in both views and clears when the cutover ends", async ({
    page,
    skipper,
  }) => {
    await page.goto(`${skipper.baseURL}/`);
    await expect(
      page.locator('[data-testid="deploy-row"][data-stack="web"][data-status="success"]'),
    ).toHaveCount(1); // startup settled: the plain first `up` started `old`

    skipper.setStackImage("web", "1.26");
    expect(await skipper.sendWebhook("refs/heads/main")).toBe(202);

    const deploying = phase(page, '[data-testid="deploy-row"]');
    await expect(deploying).toBeVisible();
    await expect(deploying).toHaveText("rolling app");

    await page.locator('[data-testid="view-toggle"] button[data-view="stacks"]').click();
    const rostered = phase(page, '[data-testid="roster-row"]');
    await expect(rostered).toHaveText("rolling app");

    // The canary turns healthy: the cutover drains `old` and the label goes.
    writeFileSync(heldTable, OLD + canary("healthy"));
    await expect(rostered).toHaveCount(0);
    await page.locator('[data-testid="view-toggle"] button[data-view="deploys"]').click();
    await expect(
      page.locator('[data-testid="deploy-row"][data-stack="web"][data-status="success"]'),
    ).toHaveCount(2);
    await expect(page.locator('[data-testid="rollout-phase"]')).toHaveCount(0);
  });
});

// UBE2 — a canary that never turns healthy ends the rollout as rolled_back and
// leaves no label behind (the Go path is P25).
test.describe("UBE2: a failed cutover leaves no label", () => {
  test.use({
    startOptions: {
      stacks: ["web"],
      initialCompose: { web: TRAEFIK_COMPOSE },
      discovery: { repoConfig: ROLLOUT_CONFIG(1) },
      stubEnv: { STUB_DOCKER_ROLLOUT_FILE: failedTable, STUB_DOCKER_CANARY_HEALTH: "unhealthy" },
    },
  });

  test("the row settles as rolled_back with no rollout-phase", async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);
    skipper.setStackImage("web", "1.26");
    expect(await skipper.sendWebhook("refs/heads/main")).toBe(202);

    // The rolled_back row is the positive signal that the cutover ran and ended.
    await expect(
      page.locator('[data-testid="deploy-row"][data-stack="web"][data-status="rolled_back"]'),
    ).toHaveCount(1);
    await expect(page.locator('[data-testid="rollout-phase"]')).toHaveCount(0);
  });
});
