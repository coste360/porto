import path from 'node:path';

import { expect, test } from '@playwright/test';

import { login } from '../support/session';
import { adminApi, createMapWithPier, marinaApi, uniq } from '../support/registry';

const BERTHS_CSV = path.resolve(import.meta.dirname, '..', 'fixtures', 'berths.csv');

test('imports berths from CSV onto a pier: dry run report, then apply', async ({ page }) => {
  await login(page);
  const map = await createMapWithPier(page, `Import ${uniq()}`, 'F');
  const shell = new URL(page.url()).pathname.replace(/\/$/, '');
  await page.goto(`${shell}/maps/${map.id}/import-berths`);

  await page.getByLabel('File CSV').setInputFiles(BERTHS_CSV);
  await expect(page.getByText('berths.csv: 30 righe di dati.')).toBeVisible();
  await page.getByRole('button', { name: 'Continua' }).click();
  await expect(page.getByText(/Anteprima delle prime righe/)).toBeVisible();
  await page.getByRole('button', { name: 'Continua' }).click();
  await page.getByRole('button', { name: 'Prova senza salvare' }).click();

  await expect(page.getByText('Risultato della prova')).toBeVisible();
  await expect(page.getByTestId('report-total')).toHaveText('30');
  await expect(page.getByTestId('report-valid')).toHaveText('28');
  await expect(page.getByTestId('report-rejected')).toHaveText('2');
  const download = page.waitForEvent('download');
  await page.getByRole('button', { name: 'Scarica il report (CSV)' }).click();
  expect((await download).suggestedFilename()).toMatch(/\.csv$/);

  await page.getByRole('button', { name: 'Importa' }).click();
  await expect(page.getByText('Importazione completata')).toBeVisible();
  await expect(page.getByRole('link', { name: "Apri la mappa nell'editor" })).toBeVisible();

  const { data } = await adminApi<{
    berths: { code: string }[];
    version: number;
  }>(page, 'GET', `${marinaApi(page)}/maps/${map.id}`);
  expect(data.berths).toHaveLength(28);
  expect(data.version).toBeGreaterThan(map.version);
});
