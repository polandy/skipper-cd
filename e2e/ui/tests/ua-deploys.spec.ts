import { test, expect } from '../fixtures/test';

// Maske A: Deploys-View. See docs/e2e-tests.md §4.2.

// UA1 — Row lifecycle. A stack that deploys (held in `deploying`, then released
// to `success`) appears as one newest-first row that mutates in place — not a
// duplicate. This is also the UI-harness smoke test: it proves the browser
// renders live SSE deploy state driven by the real backend.
test('UA1: deploy row transitions deploying → success in place', async ({ page, skipper }) => {
  await page.goto(`${skipper.baseURL}/`);

  const rows = page.locator('[data-testid="deploy-row"][data-stack="web"]');

  // The startup deploy already produced one success row for `web`.
  await expect(rows).toHaveCount(1);
  await expect(rows.first()).toHaveAttribute('data-status', 'success');

  // Hold the next `up` so the `deploying` state is observable, then push a change.
  skipper.hold();
  skipper.setStackImage('web', '1.26');
  expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);

  // A new row is prepended (newest first) and shows `deploying`.
  await expect(rows.first()).toHaveAttribute('data-status', 'deploying');
  await expect(rows.first().locator('[data-testid="status-badge"]')).toContainText('deploying');
  await expect(rows).toHaveCount(2);

  // Release the `up`: the same row mutates in place to `success` — still 2 rows,
  // no third row appended.
  skipper.release();
  await expect(rows.first()).toHaveAttribute('data-status', 'success');
  await expect(rows).toHaveCount(2);
});
