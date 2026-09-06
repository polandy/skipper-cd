import { test, expect } from '../fixtures/test';
import type { Page, Locator } from '@playwright/test';

// Maske AX: which containers a deploy's change reached. See dev-docs/e2e-tests.md
// §4.51.
//
// A deploy that moves no image still redeploys the whole stack, and the row used
// to be able to say only "1 file". The event now carries `file_changes` — per
// changed file its kind and the services it reached (ADR-0059) — and the Changes
// column renders a chip per kind. Every case here is driven by a real webhook
// push, so the attribution comes out of the backend's own comparison of the
// deployed revision against the pushed one, not a fixture.

const WEB_COMPOSE = `services:
  app:
    image: nginx:1.25
  web-restic:
    image: restic:1.18
    environment:
      - BACKUP_CRON=0 4 * * *
`;

// The same compose with two variables added to the sidecar only — the change
// that started this: no image moves, and only one of the two containers is
// touched.
const WEB_COMPOSE_SIDECAR_ENV = `services:
  app:
    image: nginx:1.25
  web-restic:
    image: restic:1.18
    environment:
      - BACKUP_CRON=0 4 * * *
      - CHECK_CRON=0 12 9 * *
      - RESTIC_DATA_SUBSET=2G
`;

test.use({
  startOptions: {
    stacks: ['web', 'api'],
    initialCompose: { web: WEB_COMPOSE },
    // A shared env file both stacks read: compose passes it project-wide, so a
    // change to it reaches every service and must read as stack-wide.
    repoFiles: { 'shared.env': 'TZ=Europe/Berlin\n' },
    envFiles: { web: ['shared.env'], api: ['shared.env'] },
  },
});

const successRow = (page: Page, stack: string): Locator =>
  page.locator(`[data-testid="deploy-row"][data-stack="${stack}"][data-status="success"]`).first();

const changes = (page: Page, stack: string): Locator =>
  successRow(page, stack).locator('[data-testid="svc-delta"]');

// Every stack already has its startup deploy row, so the pushed one is the
// stack's second — waiting for that count is what makes `.first()` the row this
// push produced, rather than racing the feed with a timing assumption.
async function pushedRowSettled(page: Page, stack: string): Promise<void> {
  await expect(
    page.locator(`[data-testid="deploy-row"][data-stack="${stack}"][data-status="success"]`),
  ).toHaveCount(2);
}

// UAX1 — the case the feature exists for: a sidecar gains two environment
// variables, the whole stack redeploys, and the row names the one container the
// change actually reached.
test.describe('UAX1: a change with no new image', () => {
  test('the row names the container the compose change reached', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);

    skipper.setStackCompose('web', WEB_COMPOSE_SIDECAR_ENV);
    expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);
    await pushedRowSettled(page, 'web');

    const row = successRow(page, 'web');
    const chip = changes(page, 'web').locator('.tag-delta');
    await expect(chip).toHaveCount(1);
    await expect(chip.locator('.td-kind')).toHaveText('compose');
    await expect(chip.locator('.td-svc')).toHaveText('web-restic');
    await expect(chip).toHaveAttribute('aria-label', 'compose changed for web-restic');
    // The untouched service is not named — that is the whole point of the chip.
    await expect(chip).not.toContainText('app');
    // It lives in the row's own Changes column, aligned with its header.
    const chipBox = await chip.boundingBox();
    const headBox = await page.locator('.event-list-header .col-version').boundingBox();
    expect(chipBox && headBox).toBeTruthy();
    expect(Math.abs(chipBox!.x - headBox!.x)).toBeLessThan(4);
    // The row still counts its files: the chip answers "who", not "how much".
    await expect(row.locator('[data-testid="files-pill"]')).toContainText('1 file');
  });
});

// UAX2 — a project-wide input names no container, and says so. The shared env
// file is passed to compose for the whole project, so claiming two of the
// services would be a guess.
test.describe('UAX2: a project-wide input', () => {
  test('a changed env file reads stack-wide, not as a service', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);

    skipper.setRepoFile('shared.env', 'TZ=Europe/Berlin\nLANG=de_DE.UTF-8\n');
    expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);
    await pushedRowSettled(page, 'api');

    const chip = changes(page, 'api').locator('.tag-delta');
    await expect(chip).toHaveCount(1);
    await expect(chip.locator('.td-kind')).toHaveText('env');
    await expect(chip.locator('.td-wide')).toHaveText('stack-wide');
    await expect(chip.locator('.td-svc')).toHaveCount(0);
    await expect(chip).toHaveAttribute('aria-label', 'env changed for every service');
  });
});

// UAX3 — an image bump must look exactly as it did before this feature: the
// compose file changed, but the version chip already names that service, and a
// second chip repeating it would cost a line and say nothing.
test.describe('UAX3: an image bump grows no second chip', () => {
  test('the version chip stands alone on a bump row', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);

    skipper.setStackImage('web', '1.26');
    expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);
    await pushedRowSettled(page, 'web');

    const cell = changes(page, 'web');
    // Positive signal that the cell rendered at all — without it, "no kind chip"
    // would pass on an empty column.
    const version = cell.locator('.tag-delta', { has: page.locator('.td-new') });
    await expect(version.locator('.td-svc')).toHaveText('app');
    await expect(version.locator('.td-new')).toHaveText('1.26');
    // …and no change chip beside it: one chip in the cell, the version one.
    await expect(cell.locator('.tag-delta')).toHaveCount(1);
    await expect(cell.locator('.td-kind')).toHaveCount(0);
  });
});

// UAX4 — the same attribution one level down, beside the file it belongs to, so
// a reader in the diff never has to carry the row's chip in their head.
test.describe('UAX4: the panel attributes each file', () => {
  test('the diff file header names the services that file reached', async ({ page, skipper }) => {
    await page.goto(`${skipper.baseURL}/`);

    skipper.setStackCompose('web', WEB_COMPOSE_SIDECAR_ENV);
    expect(await skipper.sendWebhook('refs/heads/main')).toBe(202);
    await pushedRowSettled(page, 'web');

    const row = successRow(page, 'web');
    await expect(changes(page, 'web').locator('.td-svc')).toHaveText('web-restic');
    await row.locator('[data-testid="files-pill"]').click();

    const panel = page.locator('[data-testid="diff-panel"]');
    await expect(panel).toBeVisible();
    const note = panel.locator('[data-testid="file-services"]');
    await expect(note).toHaveText('web-restic');
    await expect(note).toHaveAttribute('title', 'compose change, reaching web-restic');
  });
});
