import type { Locator } from '@playwright/test';

// A deploy row's secondary actions (deploy history, container logs, deploy
// hooks) collapse behind a single ⋯ overflow menu on the newest row (T3.13).
// openRowMenu opens that menu so those actions become clickable — call it before
// clicking a history-btn / clog-btn / hooks-badge that now lives inside it.
// Picking an action closes the menu again, so a toggle-close needs a fresh open.
export async function openRowMenu(row: Locator) {
  await row.locator('[data-testid="more-btn"]').click();
}
