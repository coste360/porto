import { expect, test } from '@playwright/test';

import { marinaIdOf } from '../support/api';
import {
  billingDay,
  createBillingCustomer,
  createBillingItem,
  createManualCharge,
  pickCustomer,
} from '../support/billing';
import { selectOption } from '../support/pricing';
import { login } from '../support/session';

// M5 reminders (m5-plan §5.5, D13, §11.5): a manual run of the day lists the reminders by level; an exclusion
// (payment plan) is added for a customer and removed again. Final notices wait for approval.

test('reminders: run of the day, items by level, exclusions', async ({ page }) => {
  await login(page);
  const marinaId = marinaIdOf(page);
  const item = await createBillingItem(page.request, marinaId);
  const customer = await createBillingCustomer(page.request, marinaId);
  await createManualCharge(page.request, marinaId, customer.id, item.id, 1000, 15_000, billingDay(-8));

  await page.goto(`/admin/m/${marinaId}/reminders`);
  await page.getByRole('button', { name: 'Esegui ora' }).click();
  await expect(page.getByTestId('reminder-run').first()).toBeVisible();
  await expect(page.getByText('Diffide da approvare', { exact: true })).toBeVisible();

  await page.getByRole('tab', { name: 'Esclusioni' }).click();
  await pickCustomer(page, page, 'Cliente', customer.display_name);
  await selectOption(page, page, 'Motivo', 'Piano di rientro');
  await page.getByLabel('Nota').fill('Rientro concordato');
  await page.getByRole('button', { name: 'Escludi' }).click();
  const row = page.getByTestId('exclusion-row').filter({ hasText: customer.display_name });
  await expect(row).toHaveCount(1);
  await expect(row).toContainText('Piano di rientro');
  await row.getByRole('button', { name: 'Rimuovi' }).click();
  await expect(row).toHaveCount(0);
});
