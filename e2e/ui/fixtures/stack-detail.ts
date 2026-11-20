import type { Locator, Page } from '@playwright/test';

// A deploy row carries no controls: a stack's deploy history, hooks and
// container logs live in its Stacks detail. openStackDetail switches to the
// Stacks view, opens stack's detail and, when given, selects its tab.
export async function openStackDetail(page: Page, stack: string, tab?: string) {
  await page.locator('[data-testid="view-toggle"] button[data-view="stacks"]').click();
  await page.locator(`[data-testid="roster-row"][data-stack="${stack}"] .roster-name`).click();
  if (tab) await page.locator(`[data-testid="detail-tab"][data-tab="${tab}"]`).click();
}

// detailHome reports where row's open detail sits: 'inline' right after the
// row, 'side' in its view's side column (a viewport of 1200 px or wider), or
// null when neither holds it.
export async function detailHome(row: Locator): Promise<'inline' | 'side' | null> {
  return row.evaluate((r) => {
    if (r.nextElementSibling?.getAttribute('data-testid') === 'detail-panel') return 'inline';
    const side = r.closest('.detail-layout')?.querySelector('[data-testid="detail-side"]');
    return side?.querySelector('[data-testid="detail-panel"]') ? 'side' : null;
  });
}
