import { readFileSync } from 'node:fs';

import { expect, test } from '@playwright/test';

import { setupRenewalScenario } from '../support/contractsM4';
import { archiveOpsHarbour } from '../support/occupancy';
import { login } from '../support/session';

// Scadenzario (m4-plan §9.5): filters, bulk settle of the selection, CSV download.

let harbour: string | null = null;
test.afterEach(async ({ page }) => {
  if (harbour) await archiveOpsHarbour(page, harbour);
  harbour = null;
});

test('scadenzario filters, bulk settle and CSV', async ({ page }) => {
  test.setTimeout(4 * 60_000);
  await login(page);
  const s = await setupRenewalScenario(page, { contracts: 4 });
  harbour = s.marinaId;
  await page.goto(`/admin/m/${s.marinaId}/installments`);
  await page.getByRole('combobox', { name: 'Stato' }).click();
  await page.getByRole('option', { name: 'Tutti' }).click();
  await expect(page.getByTestId('installment-row').first()).toBeVisible();
  const all = await page.getByTestId('installment-row').count();
  expect(all).toBeGreaterThan(0);

  await page.getByRole('combobox', { name: 'Stato' }).click();
  await page.getByRole('option', { name: 'Emessa' }).click();
  await page.getByRole('checkbox', { name: 'Seleziona tutte' }).check();
  await page.getByRole('button', { name: 'Registra incasso' }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByRole('combobox', { name: 'Metodo' }).click();
  await page.getByRole('option', { name: 'Contanti' }).click();
  await dialog.getByRole('button', { name: 'Registra' }).click();
  await expect(page.getByText(/Rate incassate:/)).toBeVisible();

  const download = page.waitForEvent('download');
  await page.getByRole('button', { name: 'Esporta CSV' }).click();
  const file = await (await download).path();
  const csv = readFileSync(file, 'utf8');
  expect(csv.split('\n')[0]).toContain(';');
});
