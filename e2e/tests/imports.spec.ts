import { expect, test } from '@playwright/test';

import { archiveOpsHarbour, setupOpsHarbour } from '../support/occupancy';
import { login } from '../support/session';

// CSV imports (m4-plan §9.5, D27): customers and vessels, dry run, apply (the whole file, all or nothing), undo.

let harbour: string | null = null;
test.afterEach(async ({ page }) => {
  if (harbour) await archiveOpsHarbour(page, harbour);
  harbour = null;
});

test('import wizard: dry run, apply, undo', async ({ page }) => {
  test.setTimeout(3 * 60_000);
  await login(page);
  const h = await setupOpsHarbour(page, { berths: 2 });
  harbour = h.marinaId;
  await page.goto(`/admin/m/${h.marinaId}/imports`);
  await page.getByRole('combobox', { name: 'Cosa importi' }).click();
  await page.getByRole('option', { name: 'Clienti e imbarcazioni' }).click();
  await page.getByRole('button', { name: 'Avanti' }).click();
  await page.getByLabel('File CSV').setInputFiles('fixtures/m4/customers.csv');
  await expect(page.getByText(/customers.csv: 2 righe di dati/)).toBeVisible();
  await page.getByRole('button', { name: 'Avanti' }).click();
  await page.getByRole('button', { name: 'Verifica', exact: true }).click();
  const summary = page.getByTestId('import-summary');
  await expect(summary).toContainText('Valide');
  await expect(summary).toContainText('2');
  await page.getByRole('button', { name: /Importa 2 righe/ }).click();
  await expect(page.getByText('Importazione completata.')).toBeVisible();
  await page.getByRole('button', { name: 'Annulla importazione' }).first().click();
  await expect(page.getByText('Annullata').first()).toBeVisible();
});
