import { readFileSync } from 'node:fs';

import { expect, test } from '@playwright/test';

import { marinaIdOf } from '../support/api';
import {
  billingDay,
  billingRequest,
  createBillingCustomer,
  createBillingItem,
  createManualCharge,
  pickCustomer,
} from '../support/billing';
import { pickDate } from '../support/pricing';
import { login } from '../support/session';

// M5 export "da fatturare" (m5-plan §3.6, §11.5): wizard over this month for one customer (preview per VAT and
// origin, confirm), downloads CSV / Excel / PDF summary, then the reconciliation CSV of the invoicing software
// in dry run and applied. Generic Italian CSV: no vendor format (D4).

test('export wizard, downloads and invoice reconciliation', async ({ page, context }) => {
  await login(page);
  const marinaId = marinaIdOf(page);
  const item = await createBillingItem(page.request, marinaId);
  const customer = await createBillingCustomer(page.request, marinaId);
  const c1 = await createManualCharge(page.request, marinaId, customer.id, item.id, 1000, 12_200);
  await createManualCharge(page.request, marinaId, customer.id, item.id, 2000, 6_100);
  const charge = await billingRequest<{ number: string }>(
    page.request,
    'GET',
    `/api/admin/v1/marinas/${marinaId}/charges/${c1}`,
  );
  const today = billingDay();

  await page.goto(`/admin/m/${marinaId}/exports/new`);
  await pickDate(page, page.getByLabel('Dal', { exact: true }), `${today.slice(0, 8)}01`);
  await pickDate(page, page.getByLabel('Al', { exact: true }), today);
  await pickCustomer(page, page, 'Cliente (facoltativo)', customer.display_name);
  await page.getByRole('button', { name: 'Anteprima', exact: true }).click();
  const preview = page.getByTestId('export-preview');
  await expect(preview).toContainText('2');
  await expect(preview).toContainText('244,00');
  await expect(page.getByTestId('vat-row')).toHaveCount(1);
  await page.getByRole('button', { name: 'Conferma esportazione' }).click();
  await expect(page.getByTestId('export-done')).toContainText('Addebiti esportati: 2');

  const csvDownload = page.waitForEvent('download');
  await page.getByRole('button', { name: 'Scarica CSV' }).click();
  const csv = readFileSync(await (await csvDownload).path(), 'utf8');
  expect(csv.split(/\r?\n/)[0]).toContain('Numero addebito;');
  expect(csv).toContain(charge.number);
  const xlsxDownload = page.waitForEvent('download');
  await page.getByRole('button', { name: 'Scarica Excel' }).click();
  const xlsx = readFileSync(await (await xlsxDownload).path());
  expect(xlsx.subarray(0, 2).toString()).toBe('PK');
  const pdf = context.waitForEvent('page');
  await page.getByRole('button', { name: 'Riepilogo PDF' }).click();
  await (await pdf).close();

  // A second export of the same period finds nothing left for the customer (INV-53).
  await page.goto(`/admin/m/${marinaId}/exports/new`);
  await pickDate(page, page.getByLabel('Dal', { exact: true }), `${today.slice(0, 8)}01`);
  await pickDate(page, page.getByLabel('Al', { exact: true }), today);
  await pickCustomer(page, page, 'Cliente (facoltativo)', customer.display_name);
  await page.getByRole('button', { name: 'Anteprima', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Conferma esportazione' })).toBeDisabled();

  const [d, m, y] = [today.slice(8, 10), today.slice(5, 7), today.slice(0, 4)];
  await page.goto(`/admin/m/${marinaId}/exports/reconcile`);
  await page
    .getByLabel('Oppure incolla il contenuto')
    .fill(`numero addebito;numero fattura;data\n${charge.number};FT-E2E-1;${d}/${m}/${y}\nA0000-999999;FT-X;${d}/${m}/${y}\n`);
  await page.getByRole('button', { name: 'Prova' }).click();
  await expect(page.getByTestId('reconcile-summary')).toContainText('abbinate: 1');
  await expect(page.getByTestId('reconcile-row')).toHaveCount(2);
  await page.getByRole('button', { name: 'Applica' }).click();
  await expect(page.getByText('Riconciliazione applicata')).toBeVisible();

  await page.goto(`/admin/m/${marinaId}/charges/${c1}`);
  await expect(page.getByText('Fatturato').first()).toBeVisible();
  await expect(page.getByText('FT-E2E-1')).toBeVisible();
});
