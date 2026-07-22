import { test, expect } from '../fixtures/test';
import type { Page } from '@playwright/test';

// Maske W: the _nixos rebuild pseudo-stack row. Seeds a real _nixos deploy via a
// stubbed nixos-rebuild (harness `nixosRebuild`), then checks its affordances and
// that clicking it never dead-ends. Reproduces the "clicking _nixos does nothing"
// report. Behaviour-only.

test.use({ startOptions: { stacks: ['web'], nixosRebuild: true } });

const nixRow = (page: Page) => page.locator('[data-testid="deploy-row"][data-stack="_nixos"]');
const anyPanel = (page: Page) =>
  page.locator('.diff-panel, .files-list, [data-testid="files-panel"], [data-testid="audit-panel"]');

// UW1 — the _nixos row appears and carries only affordances that apply to it:
// no jump-to-Stacks (not in the roster) and no container-logs (not a compose
// project).
test('UW1: the _nixos row has no jump or container-logs affordance', async ({ page, skipper }) => {
  await page.goto(`${skipper.baseURL}/`);
  await expect(nixRow(page)).toBeVisible();
  await expect(nixRow(page).locator('[data-testid="jump-btn"]')).toHaveCount(0);
  await expect(nixRow(page).locator('.clog-btn:not(.hook-log-btn)')).toHaveCount(0);
});

// UW2 — clicking a real diff-bearing _nixos row opens a panel (diff / file list
// / history) — a click must never do nothing.
test('UW2: clicking the _nixos row opens a panel, never nothing', async ({ page, skipper }) => {
  await page.goto(`${skipper.baseURL}/`);
  await expect(nixRow(page).first()).toBeVisible(); // startup rebuild row

  // Push a nix change so the next _nixos row carries a real git diff (Andy's case).
  skipper.setNixConfig('{ services.foo.enable = true; }\n');
  await skipper.sendWebhook('refs/heads/main');
  // A second _nixos row lands on top.
  await expect(nixRow(page)).toHaveCount(2);

  const row = nixRow(page).first();
  // The realistic row carries a real diff (has_diffs + a files pill).
  await expect(row).toHaveAttribute('data-has-diffs', '1');
  await expect(row.locator('.files-pill')).toHaveCount(1);
  await expect(anyPanel(page)).toHaveCount(0);

  // Clicking the row body opens its diff/files panel — never nothing.
  await row.click();
  await expect(anyPanel(page)).toHaveCount(1);
});
