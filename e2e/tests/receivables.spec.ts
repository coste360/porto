import { expect, test } from '@playwright/test';

import { marinaIdOf } from '../support/api';
import {
  billingDay,
  billingMessagesTo,
  createBillingCustomer,
  createBillingItem,
  createManualCharge,
  payLinksIn,
  pickCustomer,
  setBankDetails,
  waitForMessages,
} from '../support/billing';
import { login } from '../support/session';

// M5 scadenzario (m5-plan §4.5, §5.5, §11.5): an overdue and a future charge of a customer with an email; bulk
// "Invia richiesta di pagamento" and "Sollecita" from the list; the mailbox shows the emails with the pay link.

test('scadenzario: ageing, bulk payment request and reminder emails', async ({ page }) => {
  await login(page);
  const marinaId = marinaIdOf(page);
  await setBankDetails(page.request, marinaId);
  const item = await createBillingItem(page.request, marinaId);
  const customer = await createBillingCustomer(page.request, marinaId);
  await createManualCharge(page.request, marinaId, customer.id, item.id, 1000, 20_000, billingDay(-40));
  await createManualCharge(page.request, marinaId, customer.id, item.id, 1000, 8_000, billingDay(20));

  await page.goto(`/admin/m/${marinaId}/receivables`);
  await pickCustomer(page, page, 'Cliente', customer.display_name);
  await expect(page.getByTestId('receivable-row')).toHaveCount(2);
  await expect(page.getByTestId('ageing-d31_60')).toContainText('200,00');
  await expect(page.getByTestId('ageing-not_due')).toContainText('80,00');

  const before = billingMessagesTo(customer.email).length;
  await page.getByRole('checkbox', { name: 'Seleziona tutti' }).check();
  await page.getByRole('button', { name: 'Invia richiesta di pagamento' }).click();
  await expect(page.getByText(/Richieste create: 1/)).toBeVisible();
  const mails = await waitForMessages(customer.email, before + 1);
  const request = mails[mails.length - 1]!;
  expect(payLinksIn(request).length).toBeGreaterThan(0);
  expect(request).toContain('IT60X0542811101000000123456');

  await page.getByRole('checkbox', { name: 'Seleziona tutti' }).check();
  await page.getByRole('button', { name: 'Sollecita' }).click();
  await expect(page.getByText(/Solleciti in coda: [1-9]/)).toBeVisible();
  await waitForMessages(customer.email, before + 2);

  await page.goto(`/admin/m/${marinaId}/payment-requests`);
  // The bulk request is the one manual row; the reminders carry their own requests (origin "Sollecito").
  const requests = page.getByTestId('payment-request-row').filter({ hasText: customer.display_name });
  await expect(requests.filter({ hasText: 'Manuale' })).toHaveCount(1);
  await expect(requests.filter({ hasText: 'Sollecito' })).not.toHaveCount(0);

  await page.goto(`/admin/m/${marinaId}/receivables`);
  await page.getByRole('tab', { name: 'Sospesi per cliente' }).click();
  await expect(
    page.getByTestId('ageing-row').filter({ hasText: customer.display_name }),
  ).toContainText('280,00');
});
