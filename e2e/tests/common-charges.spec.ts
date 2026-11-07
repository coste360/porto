import { expect, test } from '@playwright/test';

import { adminRequest, marinaApi } from '../support/map';
import { archiveOpsHarbour } from '../support/occupancy';
import { setupRightsHarbour } from '../support/rights';
import { login } from '../support/session';

// Common charges (m4-plan §9.5): millesimal table from CSV with the live Σ indicator, activation at 1000 ‰, a
// budget with an item, an advance run whose allocation sums to the item exactly, statements.

let harbour: string | null = null;
test.afterEach(async ({ page }) => {
  if (harbour) await archiveOpsHarbour(page, harbour);
  harbour = null;
});

test('tables CSV import, Σ indicator, budget, advance run, statements', async ({ page }) => {
  test.setTimeout(5 * 60_000);
  await login(page);
  const h = await setupRightsHarbour(page, { berths: 8 });
  harbour = h.marinaId;
  const berths = await adminRequest<{ items: { id: string; code: string }[] }>(
    page,
    'GET',
    `${marinaApi(h.marinaId)}/berths?limit=200`,
  );
  const codes = berths.data.items.map((b) => b.code);
  // 8 berths: 125 ‰ each = 1000 ‰; the first row of the file is wrong on purpose (rejected).
  const csv = ['Posto;Millesimi', 'ZZ-99;10', ...codes.map((c) => `${c};125`)].join('\n');

  await page.goto(`/admin/m/${h.marinaId}/common-charges`);
  await page.getByLabel('Nome').fill('Generale');
  await page.getByLabel('Codice').fill('GEN');
  await page.getByRole('button', { name: 'Nuova tabella' }).click();
  const sum = page.getByTestId('millesimal-sum');
  await expect(sum).toHaveText(/Somma 0 ‰/);
  await page.getByLabel(/Importa CSV/).setInputFiles({
    name: 'millesimi.csv',
    mimeType: 'text/csv',
    buffer: Buffer.from(csv),
  });
  await expect(sum).toHaveText(/Somma 1000 ‰ \(mancano 0\)/);
  await expect(page.getByRole('status')).toContainText('1 posti sconosciuti');
  await page.getByRole('button', { name: 'Salva', exact: true }).click();
  await expect(page.getByText('Tabella salvata.')).toBeVisible();
  await page.getByRole('button', { name: 'Attiva', exact: true }).click();
  await expect(page.getByText('Attiva').first()).toBeVisible();

  await page.getByRole('tab', { name: 'Preventivi e ripartizioni' }).click();
  await page.getByRole('button', { name: 'Nuovo preventivo' }).click();
  await page.getByLabel('Voce').fill('Pulizia pontili');
  await page.getByRole('combobox', { name: 'Tabella' }).click();
  await page.getByRole('option', { name: 'GEN' }).click();
  await page.getByLabel('Preventivo').fill('1000,01');
  await page.getByRole('button', { name: 'Aggiungi voce' }).click();
  await page.getByRole('button', { name: 'Approva' }).click();
  await page.getByRole('button', { name: 'Calcola ripartizione' }).click();
  const run = page.getByTestId('common-run').first();
  await expect(run).toBeVisible();
  // Advance share 50 %: 500,01 € (half-even) allocated exactly.
  await expect(run.getByTestId('run-total')).toHaveText(/500,0[01]/);
  await expect(run.getByTestId('run-allocated')).toHaveText(
    await run.getByTestId('run-total').innerText(),
  );
  await run.getByRole('button', { name: 'Emetti' }).click();
  const pdf = page.waitForEvent('popup');
  // The PDF per holder needs a holder (API ?customer_id=); the run offers the PDF per item and the CSV of every holder.
  await run.getByRole('button', { name: 'Rendiconto per voce' }).click();
  await (await pdf).close();
});
