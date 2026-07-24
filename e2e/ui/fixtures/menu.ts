import type { Locator } from '@playwright/test';

// A row's secondary actions collapse behind a single ⋯ overflow menu: the
// newest deploy row folds deploy history / container logs / deploy hooks (T3.13),
// and the Stacks/roster row folds container logs / deploy hooks (T3.13b).
// openRowMenu opens that menu so those actions become clickable — call it before
// clicking a history-btn / clog-btn / hooks-badge that now lives inside it.
// Picking an action closes the menu again, so a toggle-close needs a fresh open.
export async function openRowMenu(row: Locator) {
  await row.locator('[data-testid="more-btn"]').click();
}
