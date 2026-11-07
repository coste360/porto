import { expect, test } from '@playwright/test';

import { marinaIdOf } from '../support/api';
import {
  billingDay,
  createBillingCustomer,
  createBillingItem,
  createManualCharge,
} from '../support/billing';
import { login } from '../support/session';

// M5 matching (m5-plan §4.2, §11.5): a bank transfer larger than two open charges is auto-filled oldest due
// first, a row never takes more than its residual, the rest becomes customer credit; the credit is refunded.

test('record payment: oldest first, residual cap, credit, refund of the credit', async ({ page }) => {
  await login(page);
  const marinaId = marinaIdOf(page);
  const item = await createBillingItem(page.request, marinaId);
  const customer = await createBillingCustomer(page.request, marinaId);
  await createManualCharge(page.request, marinaId, customer.id, item.id, 1000, 10_000, billingDay(10));
  await createManualCharge(page.request, marinaId, customer.id, item.id, 1000, 5_000, billingDay(-5));

  await page.goto(`/admin/m/${marinaId}/payments`);
  await page.getByRole('button', { name: 'Registra pagamento' }).click();
  const dialog = page.getByTestId('record-payment-dialog');
  await dialog.getByRole('combobox', { name: 'Cliente' }).click();
  await page.getByPlaceholder('Cerca per nome').fill(customer.display_name.split(' ').pop()!);
  await page.getByRole('option', { name: customer.display_name }).click();
  await expect(dialog.getByTestId('match-row')).toHaveCount(2);
  await dialog.getByLabel('Importo', { exact: true }).fill('180');
  const rows = dialog.getByTestId('match-row');
  // The older charge (due 5 days ago) comes first and is filled first.
  await expect(rows.nth(0).getByRole('textbox')).toHaveValue('50,00');
  await expect(rows.nth(1).getByRole('textbox')).toHaveValue('100,00');
  await expect(dialog.getByTestId('match-credit')).toContainText('30,00');
  await rows.nth(0).getByRole('textbox').fill('999');
  await expect(rows.nth(0).getByRole('textbox')).toHaveValue('50,00');
  await dialog.getByRole('button', { name: 'Registra' }).click();
  await expect(page.getByText(/Pagamento P\d{4}-\d{6} registrato/)).toBeVisible();

  await page.getByTestId('payment-row').first().getByRole('link').click();
  await expect(page.getByTestId('payment-match')).toHaveCount(2);
  await expect(page.getByTestId('payment-unallocated')).toContainText('30,00');

  await page.getByRole('button', { name: 'Rimborsa' }).click();
  const refund = page.getByTestId('refund-dialog');
  await expect(refund.getByLabel('Importo da rimborsare')).toHaveValue('30,00');
  await refund.getByLabel('Motivo').fill('Restituzione eccedenza');
  await refund.getByRole('button', { name: 'Rimborsa' }).click();
  await expect(page.getByText('Rimborso registrato')).toBeVisible();
  await expect(page.getByTestId('payment-refund')).toHaveCount(1);
  await expect(page.getByTestId('payment-unallocated')).toContainText('0,00');
});
