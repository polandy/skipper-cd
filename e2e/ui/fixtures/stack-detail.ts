import type { Page } from '@playwright/test';

// A deploy row carries no controls: a stack's deploy history, hooks and
// container logs live in its Stacks detail. openStackDetail switches to the
// Stacks view, opens stack's detail and, when given, selects its tab.
export async function openStackDetail(page: Page, stack: string, tab?: string) {
  await page.locator('[data-testid="view-toggle"] button[data-view="stacks"]').click();
  await page.locator(`[data-testid="roster-row"][data-stack="${stack}"] .roster-name`).click();
  if (tab) await page.locator(`[data-testid="detail-tab"][data-tab="${tab}"]`).click();
}
