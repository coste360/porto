import { readFileSync } from 'node:fs';

import { expect, test } from '@playwright/test';

import { marinaIdOf } from '../support/api';
import { createBillingCustomer, createBillingItem } from '../support/billing';
import { login } from '../support/session';

// M5 charges (m5-plan §9.1, §11.5): a manual charge from the UI with a live VAT preview, the saved views, the
// CSV of the list, the cancel of an untouched charge with a reason. Manual driver only, no network.

test('manual charge: create with VAT preview, find it in "Da fatturare", cancel it', async ({
  page,
}) => {
  await login(page);
  const marinaId = marinaIdOf(page);
  const item = await createBillingItem(page.request, marinaId);
  const customer = await createBillingCustomer(page.request, marinaId);

  await page.goto(`/admin/m/${marinaId}/charges/new`);
  await page.getByRole('combobox', { name: 'Cliente' }).click();
  await page.getByPlaceholder('Cerca per nome').fill(customer.display_name.split(' ').pop()!);
  await page.getByRole('option', { name: customer.display_name }).click();
  await page.getByRole('combobox', { name: 'Articolo della riga 1' }).click();
  await page.getByRole('option', { name: new RegExp(item.code) }).click();
  await page.getByLabel('Quantità della riga 1').fill('2');
  await page.getByLabel('Prezzo unitario della riga 1').fill('61');
  await expect(page.getByTestId('lines-preview')).toContainText('122,00');
  await expect(page.getByTestId('lines-preview')).toContainText('22,00');
  await page.getByRole('button', { name: 'Crea addebito' }).click();

  await expect(page.getByRole('heading', { name: /Addebito A\d{4}-\d{6}/ })).toBeVisible();
  const number = (await page.getByRole('heading', { name: /Addebito A/ }).innerText())
    .replace('Addebito ', '')
    .trim();
  await expect(page.getByTestId('charge-residual')).toContainText('122,00');
  await expect(page.getByTestId('charge-line')).toHaveCount(1);
  await expect(page.getByTestId('vat-row')).toHaveCount(1);

  await page.goto(`/admin/m/${marinaId}/charges`);
  await page.getByRole('radio', { name: 'Da fatturare' }).click();
  await page.getByLabel('Numero').fill(number);
  await expect(page.getByTestId('charge-row')).toHaveCount(1);
  const download = page.waitForEvent('download');
  await page.getByRole('button', { name: 'Esporta CSV' }).click();
  const csv = readFileSync(await (await download).path(), 'utf8');
  expect(csv).toContain(number);

  await page.getByRole('link', { name: number }).click();
  await page.getByRole('button', { name: 'Annulla addebito' }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByLabel('Motivo').fill('Inserito per errore');
  await dialog.getByRole('button', { name: 'Annulla addebito' }).click();
  await expect(page.getByText('Annullato').first()).toBeVisible();
  await expect(page.getByRole('button', { name: 'Rettifica' })).toHaveCount(0);
});
