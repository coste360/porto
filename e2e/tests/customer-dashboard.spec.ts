import { expect, test } from '@playwright/test';

import { marinaIdOf } from '../support/api';
import {
  billingDay,
  createBillingCustomer,
  createBillingItem,
  createManualCharge,
  recordPayment,
} from '../support/billing';
import { login } from '../support/session';

// M5 customer dashboard and statement (spec 11.4, m5-plan §4.5, §11.5): balance and ageing on the "Cruscotto"
// tab; the "Estratto conto" closing balance equals the balance; the statement PDF opens.

test('customer dashboard: balance, ageing and statement of account', async ({ page, context }) => {
  await login(page);
  const marinaId = marinaIdOf(page);
  const item = await createBillingItem(page.request, marinaId);
  const customer = await createBillingCustomer(page.request, marinaId);
  const old = await createManualCharge(page.request, marinaId, customer.id, item.id, 1000, 30_000, billingDay(-35));
  await createManualCharge(page.request, marinaId, customer.id, item.id, 1000, 10_000, billingDay(15));
  await recordPayment(page.request, marinaId, customer.id, 12_000, 'bank_transfer', [old]);

  await page.goto(`/admin/m/${marinaId}/customers/${customer.id}`);
  await page.getByRole('tab', { name: 'Cruscotto' }).click();
  await expect(page.getByTestId('dashboard-balance')).toContainText('280,00');
  await expect(page.getByTestId('ageing-d31_60')).toContainText('180,00');
  await expect(page.getByTestId('ageing-not_due')).toContainText('100,00');

  await page.getByRole('tab', { name: 'Estratto conto' }).click();
  await expect(page.getByTestId('statement-row')).toHaveCount(3);
  await expect(page.getByTestId('statement-closing')).toContainText('280,00');
  const pdf = context.waitForEvent('page');
  await page.getByRole('button', { name: 'Estratto conto PDF' }).click();
  await (await pdf).close();
});
