import { expect, test } from '@playwright/test';

import { marinaIdOf } from '../support/api';
import { createBillingItem } from '../support/billing';
import { selectOption } from '../support/pricing';
import { login } from '../support/session';

// M5 cash desk (m5-plan §5.1, §5.3, §11.5): open with a float, counter sale to a walk-in customer with the
// commercial document and the 80 mm receipt print view, close with the counted cash and the difference, Z report.

test('cash day: open, counter sale with receipt, close with difference, Z report', async ({
  page,
  context,
}) => {
  await login(page);
  const marinaId = marinaIdOf(page);
  const item = await createBillingItem(page.request, marinaId);

  await page.goto(`/admin/m/${marinaId}/cash`);
  const open = page.getByRole('button', { name: 'Apri cassa' });
  if (await open.isVisible()) {
    await page.getByLabel('Fondo cassa').fill('100');
    await open.click();
  }
  await expect(page.getByTestId('cash-session')).toBeVisible();

  await page.getByRole('button', { name: 'Vendita al banco' }).click();
  const sale = page.getByTestId('counter-sale-dialog');
  await sale.getByRole('combobox', { name: 'Articolo della riga 1' }).click();
  await page.getByRole('option', { name: new RegExp(item.code) }).click();
  await sale.getByLabel('Prezzo unitario della riga 1').fill('12,20');
  await selectOption(page, sale, 'Metodo', 'Contanti');
  await expect(sale.getByRole('checkbox', { name: 'Emetti documento commerciale' })).toBeChecked();
  const receipt = context.waitForEvent('page');
  await sale.getByRole('button', { name: 'Incassa' }).click();
  await expect(page.getByText(/Vendita registrata: pagamento P\d{4}-\d{6}/)).toBeVisible();
  const view = await receipt;
  await view.waitForLoadState();
  await expect(view.locator('body')).toContainText('12,20');
  await expect(view.locator('body')).toContainText(/Non valido ai fini fiscali/i);
  await view.close();

  await expect(page.getByTestId('cash-payments')).not.toContainText('0,00 €');
  await page.getByRole('button', { name: 'Chiudi cassa' }).click();
  const close = page.getByTestId('close-session-dialog');
  const expected = (await close.getByTestId('close-row').first().locator('td').nth(1).innerText())
    .replace(/[^\d,]/g, '');
  await close.getByLabel('Contato Contanti').fill(expected);
  await expect(close.getByTestId('close-difference')).toContainText('0,00');
  await close.getByLabel('Contato Contanti').fill('1');
  await expect(close.getByTestId('close-difference')).toContainText('-');
  await close.getByRole('button', { name: 'Chiudi cassa' }).click();
  await expect(close.getByText(/Cassa chiusa/)).toBeVisible();
  const z = context.waitForEvent('page');
  await close.getByRole('button', { name: 'Report Z (stampa)' }).click();
  const zView = await z;
  await zView.waitForLoadState();
  await expect(zView.locator('body')).toContainText('12,20');
  await zView.close();

  await page.keyboard.press('Escape');
  await page.getByRole('tab', { name: 'Storico' }).click();
  await expect(page.getByTestId('cash-history-row').first()).toBeVisible();
});
