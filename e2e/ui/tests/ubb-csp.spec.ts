import { test, expect } from "../fixtures/test";
import type { Page } from "@playwright/test";

// Maske BB: the app runs under its strict Content-Security-Policy (ADR-0067).
// See dev-docs/e2e-tests.md §4.55.
//
// A listener installed before any page script records every
// securitypolicyviolation. The test walks the views, opens a diff panel, then
// injects one inline script itself: that one violation proves the recorder
// works, and that it is the only one recorded is the real assertion.
// Behaviour-only (no snapshot).

type Violation = { directive: string; blocked: string; sample: string };

const viewBtn = (page: Page, view: string) =>
  page.locator(`[data-testid="view-toggle"] button[data-view="${view}"]`);
const diffRow = (page: Page) =>
  page.locator(
    '[data-testid="deploy-row"][data-stack="web"][data-status="success"][data-has-diffs="1"]',
  );

const violations = (page: Page) =>
  page.evaluate(() => (window as { __csp?: Violation[] }).__csp ?? []);

test.describe("Maske BB: the app under its CSP", () => {
  test("UBB1: headers are set and no view trips the policy", async ({
    page,
    skipper,
  }) => {
    await page.addInitScript(() => {
      localStorage.setItem("colorScheme", "light");
      const w = window as { __csp?: Violation[] };
      w.__csp = [];
      document.addEventListener("securitypolicyviolation", (e) => {
        w.__csp!.push({
          directive: e.effectiveDirective,
          blocked: e.blockedURI,
          sample: e.sample,
        });
      });
    });

    const resp = await page.goto(`${skipper.baseURL}/`);
    const headers = resp!.headers();
    expect(headers["content-security-policy"]).toContain("script-src 'self'");
    expect(headers["content-security-policy"]).toContain(
      "frame-ancestors 'none'",
    );
    expect(headers["x-frame-options"]).toBe("DENY");
    expect(headers["x-content-type-options"]).toBe("nosniff");
    expect(headers["referrer-policy"]).toBe("same-origin");

    // The external pre-paint script ran: it applies the saved light scheme.
    await expect(page.locator("html")).toHaveClass(/\blight\b/);

    skipper.setStackImage("web", "1.26");
    expect(await skipper.sendWebhook("refs/heads/main")).toBe(202);
    await expect(diffRow(page)).toHaveCount(1);
    await diffRow(page).locator('[data-testid="files-pill"]').click();
    await expect(page.locator('[data-testid="diff-panel"]')).toHaveCount(1);

    await viewBtn(page, "stacks").click();
    await expect(
      page.locator('[data-testid="roster-row"][data-stack="web"]'),
    ).toHaveCount(1);
    await viewBtn(page, "logs").click();
    await viewBtn(page, "deploys").click();

    await page.evaluate(() => {
      const s = document.createElement("script");
      s.textContent = "window.__inlineRan = true";
      document.head.appendChild(s);
    });
    // Violations are reported in order, so once the injected one is in, any
    // earlier one from the views would be too.
    await expect
      .poll(async () => (await violations(page)).map((v) => v.directive))
      .toContain("script-src-elem");
    expect(await violations(page)).toHaveLength(1);
    expect(
      await page.evaluate(
        () => (window as { __inlineRan?: boolean }).__inlineRan,
      ),
    ).toBeUndefined();
  });
});
