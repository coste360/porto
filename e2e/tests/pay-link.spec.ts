import { expect, test } from '@playwright/test';

import { marinaIdOf } from '../support/api';
import {
  billingRequest,
  createBillingCustomer,
  createBillingItem,
  createManualCharge,
  payLinkFromMail,
  setBankDetails,
} from '../support/billing';
import { expectNoSeriousViolations } from '../support/registry';
import { login, logout } from '../support/session';

// M5 public pay page (m5-plan §5.5, §9.1, §11.5): a payment request emailed with the manual driver only; the
// customer opens the link on a phone: amount, charges, bank details with the copy button, no online button.

test('pay link: manual-only page with bank details, phone width, axe clean', async ({ page }) => {
  await login(page);
  const marinaId = marinaIdOf(page);
  await setBankDetails(page.request, marinaId);
  const item = await createBillingItem(page.request, marinaId);
  const customer = await createBillingCustomer(page.request, marinaId);
  const chargeId = await createManualCharge(page.request, marinaId, customer.id, item.id, 1000, 24_400);
  await billingRequest(page.request, 'POST', `/api/admin/v1/marinas/${marinaId}/payment-requests`, {
    body: { customer_id: customer.id, charge_ids: [chargeId], send: true },
  });
  const link = await payLinkFromMail(customer.email, 20_000);
  await logout(page);

  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto(new URL(link).pathname);
  await expect(page.getByTestId('pay-amount')).toContainText('244,00');
  await expect(page.getByTestId('pay-charge')).toHaveCount(1);
  const bank = page.getByTestId('pay-bank');
  await expect(bank).toContainText('IT60X0542811101000000123456');
  await expect(bank.getByRole('button', { name: 'Copia IBAN' })).toBeVisible();
  await expect(page.getByTestId('pay-online')).toHaveCount(0);
  const scrollWidth = await page.evaluate(() => document.documentElement.scrollWidth);
  expect(scrollWidth).toBeLessThanOrEqual(390);
  await expectNoSeriousViolations(page, 'pay page');

  await page.goto(`${new URL(link).pathname}/return`);
  await expect(page.getByTestId('pay-verifying')).toBeVisible();

  const tampered = new URL(link).pathname.replace(/.$/, (c) => (c === 'A' ? 'B' : 'A'));
  await page.goto(tampered);
  await expect(page.getByText('Link non valido')).toBeVisible();
});
