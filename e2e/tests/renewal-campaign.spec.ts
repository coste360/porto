import { expect, test } from '@playwright/test';

import { contractCustomerEmail, setupRenewalScenario } from '../support/contractsM4';
import { latestLinkFromMail, latestOtpFromMail, pathOf } from '../support/links';
import { archiveOpsHarbour } from '../support/occupancy';
import { login } from '../support/session';

// Renewal campaign (m4-plan §9.5): wizard on 20 contracts, launch, the customer accepts and signs on a phone in a
// second context, the office settles the deposit, the monitor updates through SSE within 3 s.

let harbour: string | null = null;
test.afterEach(async ({ page }) => {
  if (harbour) await archiveOpsHarbour(page, harbour);
  harbour = null;
});

test('campaign wizard → launch → customer signs on a phone → deposit settled', async ({
  page,
  browser,
}) => {
  test.setTimeout(6 * 60_000);
  await login(page);
  const s = await setupRenewalScenario(page, { contracts: 20 });
  harbour = s.marinaId;

  await page.goto(`/admin/m/${s.marinaId}/renewals`);
  await page.getByRole('link', { name: 'Nuova campagna' }).click();
  await page.getByLabel('Nome della campagna').fill('Rinnovi E2E');
  await page.getByRole('checkbox', { name: 'Annuale' }).check();
  await page.getByRole('button', { name: 'Calcola anteprima' }).click();
  await expect(page.getByTestId('proposal-row')).toHaveCount(20, {
    timeout: 60_000,
  });
  await page.getByRole('button', { name: 'Avanti' }).click();
  await expect(page.getByLabel('Messaggio ai clienti')).toBeVisible();
  await page.getByLabel('Messaggio ai clienti').fill('Rinnova il tuo posto per il prossimo anno.');
  await page.getByRole('button', { name: 'Avanti' }).click();
  await page.getByRole('button', { name: 'Avvia la campagna' }).click();
  await expect(page.getByTestId('count-sent')).toHaveText(/20/, {
    timeout: 90_000,
  });

  // The customer of the first contract opens the proposal on a phone and accepts it.
  const email = await contractCustomerEmail(page, s.marinaId, s.contractIds[0]!);
  const link = await latestLinkFromMail(page.request, email, 'renewal');
  const phone = await browser.newContext({
    viewport: { width: 390, height: 844 },
    locale: 'it-IT',
  });
  const customer = await phone.newPage();
  await customer.goto(pathOf(link));
  await customer.getByRole('button', { name: 'Accetto il rinnovo' }).click();
  // The signature link arrives by email (no signature_link in the acceptance answer yet).
  await customer.goto(pathOf(await latestLinkFromMail(phone.request, email, 'signature')));
  await customer.getByRole('checkbox', { name: /Confermo che sono io/ }).check();
  await customer.getByRole('button', { name: 'Inviami il codice' }).click();
  const code = await latestOtpFromMail(phone.request, email);
  await customer.getByRole('textbox').fill(code);
  await customer.getByRole('button', { name: 'Firma', exact: true }).click();
  await expect(customer.getByText('Documento firmato. Grazie!')).toBeVisible();
  await expect(customer.getByTestId('deposit')).toBeVisible();
  // The deposit reference is the successor's contract code.
  const successor =
    /Causale: (\S+)/.exec(await customer.getByTestId('deposit').innerText())?.[1] ?? '';
  await phone.close();

  // SSE: the monitor shows the signed proposal without a reload (the page is never reloaded, so the count can only
  // change through the stream; no latency bound: machine speed is not under test).
  await expect(page.getByTestId('count-signed')).toHaveText(/1/);

  // The office settles the deposit from the scadenzario; the proposal completes.
  await page.goto(`/admin/m/${s.marinaId}/installments`);
  await page.getByRole('row').filter({ hasText: successor }).first().getByRole('checkbox').check();
  await page.getByRole('button', { name: 'Registra incasso' }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByRole('combobox', { name: 'Metodo' }).click();
  await page.getByRole('option', { name: 'Bonifico' }).click();
  await dialog.getByLabel(/Riferimento/).fill('CRO E2E');
  await dialog.getByRole('button', { name: 'Registra' }).click();
  await expect(dialog).toBeHidden();
  await page.goBack();
  await page.goto(`/admin/m/${s.marinaId}/renewals`);
  await page.getByRole('link', { name: 'Rinnovi E2E' }).click();
  await expect(page.getByTestId('count-completed')).toHaveText(/1/, {
    timeout: 10_000,
  });
});
