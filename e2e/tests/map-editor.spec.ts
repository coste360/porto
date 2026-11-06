import AxeBuilder from '@axe-core/playwright';
import { expect, test, type Page } from '@playwright/test';

import { waitForResetLink } from '../support/mail';
import {
  addPierThroughApi,
  adminRequest,
  createMap,
  getMap,
  marinaApi,
  marinaOf,
  type MapDoc,
} from '../support/map';
import { login } from '../support/session';

async function setupMap(page: Page, name: string): Promise<{ marinaId: string; map: MapDoc }> {
  await login(page);
  const marinaId = marinaOf(page);
  const created = await createMap(page, marinaId, `${name} ${Date.now()}`);
  const map = await addPierThroughApi(page, marinaId, created.id, {
    code: 'A',
    origin: { x: 10_000, y: 30_000 },
    berthsPerSide: 5,
    keyPrefix: 'e2e-',
  });
  return { marinaId, map };
}

const toolbar = (page: Page) => page.getByRole('toolbar', { name: "Strumenti dell'editor" });

test('multi-select, bulk price class, groups, align, undo/redo and renumber', async ({ page }) => {
  const { marinaId, map } = await setupMap(page, 'Bulk');
  const group = (
    await adminRequest<{ id: string }>(page, 'POST', `${marinaApi(marinaId)}/berth-groups`, {
      body: {
        code: `T${Date.now() % 100000}`,
        name: { it: 'Transiti' },
        kind: 'transit',
      },
    })
  ).data;
  await page.goto(`/admin/m/${marinaId}/maps/${map.id}/edit`);
  await expect(page.getByTestId('berth-count')).toHaveText('Posti: 10');

  await page.getByRole('tab', { name: 'Elenco' }).click();
  await page.getByRole('button', { name: 'Seleziona tutti i posti' }).click();
  await expect(page.getByTestId('editor-status')).toContainText('Selezionati: 10');

  await page.getByRole('tab', { name: 'Proprietà' }).click();
  const priceClass = page.getByLabel('Categoria di prezzo');
  await priceClass.fill('L14');
  await priceClass.press('Enter');
  await page.getByRole('checkbox', { name: /^T\d+$/ }).click();

  // Align then undo and redo.
  await toolbar(page).getByRole('button', { name: 'Disponi' }).click();
  await page.getByRole('menuitem', { name: 'Allinea a sinistra' }).click();
  await toolbar(page).getByRole('button', { name: 'Annulla' }).click();
  await toolbar(page).getByRole('button', { name: 'Ripeti' }).click();
  await toolbar(page).getByRole('button', { name: 'Annulla' }).click();
  await toolbar(page).getByRole('button', { name: 'Disponi' }).click();
  await page.getByRole('menuitem', { name: 'Distribuisci in verticale' }).click();
  await toolbar(page).getByRole('button', { name: 'Annulla' }).click();

  // Renumber with a preview.
  await toolbar(page).getByRole('button', { name: 'Disponi' }).click();
  await page.getByRole('menuitem', { name: 'Rinumera' }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByLabel('Prefisso').fill('X-');
  await expect(dialog.getByRole('table', { name: 'Anteprima della rinumerazione' })).toContainText(
    'X-10',
  );
  await dialog.getByRole('button', { name: 'Rinumera' }).click();

  await toolbar(page).getByRole('button', { name: 'Salva' }).click();
  await expect(page.getByText(/Mappa salvata/)).toBeVisible();
  const saved = await getMap(page, marinaId, map.id);
  const berths = saved.berths as unknown as {
    code: string;
    price_class?: string;
    group_ids: string[];
  }[];
  expect(berths.every((b) => b.price_class === 'L14')).toBe(true);
  expect(berths.every((b) => b.group_ids.includes(group.id))).toBe(true);
  expect(berths.map((b) => b.code).sort()).toEqual(
    Array.from({ length: 10 }, (_, i) => `X-${i + 1}`).sort(),
  );
});

test('a concurrent save opens the conflict dialog', async ({ page }) => {
  const { marinaId, map } = await setupMap(page, 'Conflict');
  await page.goto(`/admin/m/${marinaId}/maps/${map.id}/edit`);
  await expect(page.getByTestId('berth-count')).toHaveText('Posti: 10');

  // Another session saves first.
  const other = await page.context().newPage();
  await other.goto(`/admin/m/${marinaId}/maps/${map.id}/edit`);
  await other.getByRole('tab', { name: 'Proprietà' }).click();
  await other.getByLabel('Nome (italiano)').fill('Rinominata');
  await other.getByLabel('Nome (italiano)').press('Enter');
  await toolbar(other).getByRole('button', { name: 'Salva' }).click();
  await expect(other.getByText(/Mappa salvata/)).toBeVisible();
  await other.close();

  await page.getByRole('tab', { name: 'Proprietà' }).click();
  await page.getByLabel('Passo della griglia').fill('1');
  await page.getByLabel('Passo della griglia').press('Enter');
  await toolbar(page).getByRole('button', { name: 'Salva' }).click();
  const conflict = page.getByRole('alertdialog');
  await expect(conflict).toContainText('modificata da un');
  await expect(conflict.getByTestId('conflict-details')).toContainText('Server: versione');
  await conflict.getByRole('button', { name: 'Ricarica' }).click();
  await expect(page.getByRole('heading', { name: /Rinominata/ })).toBeVisible();
});

test('a viewer sees the map read-only', async ({ page, browser }) => {
  const { marinaId, map } = await setupMap(page, 'Viewer');
  const email = `viewer-${Date.now()}@porto.test`;
  const password = 'e2e-viewer-password-123';
  const linkPromise = waitForResetLink(email);
  await adminRequest(page, 'POST', `${marinaApi(marinaId)}/members`, {
    body: { name: 'Vera Vista', email, role: 'viewer', locale: 'it' },
  });
  const link = new URL(await linkPromise);
  const ctx = await browser.newContext({ locale: 'it-IT' });
  const viewer = await ctx.newPage();
  await viewer.goto(link.pathname + link.hash);
  await viewer.getByLabel('Nuova password').fill(password);
  await viewer.getByLabel('Ripeti la password').fill(password);
  await viewer.getByRole('button', { name: 'Salva la password' }).click();
  await expect(viewer.getByText('Password aggiornata. Ora puoi accedere.')).toBeVisible();
  await login(viewer, email, password);

  await viewer.goto(`/admin/m/${marinaId}/maps/${map.id}`);
  await expect(viewer.getByRole('button', { name: 'Posto A-1', exact: true })).toBeVisible();
  await expect(viewer.getByRole('link', { name: /Modifica nell'editor/ })).toHaveCount(0);
  await viewer.goto(`/admin/m/${marinaId}/maps/${map.id}/edit`);
  await expect(viewer.getByText('Non hai accesso a questa sezione.')).toBeVisible();
  await ctx.close();
});

test('the print page renders in scale with a legend', async ({ page }) => {
  const { marinaId, map } = await setupMap(page, 'Print');
  await page.goto(`/admin/m/${marinaId}/maps/${map.id}/print`);
  await expect(page.getByTestId('print-scale')).toHaveText(/Scala 1:\d+/);
  await expect(page.getByRole('list', { name: 'Legenda' })).toBeVisible();
  await expect(page.getByRole('img', { name: 'Foglio di stampa della mappa' })).toBeVisible();
});

for (const scheme of ['light', 'dark'] as const) {
  test(`editor has no serious axe findings (${scheme})`, async ({ page }) => {
    await page.emulateMedia({ colorScheme: scheme });
    const { marinaId, map } = await setupMap(page, `Axe ${scheme}`);
    await page.goto(`/admin/m/${marinaId}/maps/${map.id}/edit`);
    await expect(page.getByTestId('berth-count')).toHaveText('Posti: 10');
    const result = await new AxeBuilder({ page })
      .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
      .analyze();
    const serious = result.violations
      .filter((v) => v.impact === 'serious' || v.impact === 'critical')
      .map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(' ')).join(', ')}`);
    expect(serious).toEqual([]);
  });
}
