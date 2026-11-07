import { expect, test } from '@playwright/test';

import {
  BERTH_VESSEL,
  createCustomerWithEmail,
  createQuoteViaApi,
  uniqTag,
  useOtpSignature,
} from '../support/contractsM4';
import { latestLinkFromMail, latestOtpFromMail, pathOf } from '../support/links';
import {
  archiveOpsHarbour,
  createVesselViaApi,
  marinaDay,
  setupOpsHarbour,
  setupPrices,
} from '../support/occupancy';
import { login } from '../support/session';

// Quote acceptance (m4-plan §9.5): the office sends the quote, the customer accepts it online uploading the
// insurance, signs with the email code, the contract becomes active.

let harbour: string | null = null;
test.afterEach(async ({ page }) => {
  if (harbour) await archiveOpsHarbour(page, harbour);
  harbour = null;
});

test('send → public acceptance with upload → OTP signing → contract active', async ({
  page,
  browser,
}) => {
  test.setTimeout(4 * 60_000);
  await login(page);
  const h = await setupOpsHarbour(page, { berths: 4 });
  harbour = h.marinaId;
  await setupPrices(page, h.marinaId);
  await useOtpSignature(page, h.marinaId);
  const email = `preventivo-${uniqTag()}@e2e.porto.test`;
  const customer = await createCustomerWithEmail(page, h.marinaId, email);
  const vessel = await createVesselViaApi(page, h.marinaId, BERTH_VESSEL);
  const quote = await createQuoteViaApi(page, h.marinaId, {
    customerId: customer.id,
    vesselId: vessel.id,
    berthId: h.berths[0]!.id,
    startsOn: marinaDay(10),
  });

  await page.goto(`/admin/m/${h.marinaId}/quotes/${quote.id}`);
  await page.getByRole('button', { name: 'Invia per email' }).click();
  await page.getByRole('dialog').getByRole('button', { name: 'Invia per email' }).click();
  await expect(page.getByText('Preventivo inviato al cliente.')).toBeVisible();

  const link = await latestLinkFromMail(page.request, email, 'quote');
  const ctx = await browser.newContext({
    viewport: { width: 390, height: 844 },
    locale: 'it-IT',
  });
  const c = await ctx.newPage();
  await c.goto(pathOf(link));
  await expect(c.getByTestId('link-pdf')).toBeVisible();
  const upload = c.getByLabel('Polizza assicurativa');
  if (await upload.count()) {
    await upload.setInputFiles('fixtures/m4/scan.pdf');
    await expect(c.getByText('Documento caricato.')).toBeVisible();
  }
  const registration = c.getByLabel('Matricola');
  if (await registration.count()) {
    await registration.fill('GE7001');
    await c
      .getByLabel("Scadenza dell'assicurazione")
      .fill(`${Number(marinaDay(0).slice(0, 4)) + 2}-06-30`);
  }
  await c.getByRole('button', { name: 'Accetto il preventivo' }).click();
  await expect(c.getByText(/Preventivo accettato: contratto/)).toBeVisible();
  // The signature link arrives by email (the acceptance answer carries no signature_link: signer links are
  // created and emailed after the commit and stored hashed).
  await c.goto(pathOf(await latestLinkFromMail(ctx.request, email, 'signature')));
  await c.getByRole('checkbox', { name: /Confermo che sono io/ }).check();
  await c.getByRole('button', { name: 'Inviami il codice' }).click();
  await c.getByRole('textbox').fill(await latestOtpFromMail(ctx.request, email));
  await c.getByRole('button', { name: 'Firma', exact: true }).click();
  await expect(c.getByText('Documento firmato. Grazie!')).toBeVisible();
  await ctx.close();

  await page.reload();
  await page.getByRole('link', { name: 'Apri il contratto' }).click();
  await expect(page.getByText('Attivo', { exact: true })).toBeVisible();
  await page.getByRole('tab', { name: 'Firma' }).click();
  await expect(page.getByTestId('signature-request')).toContainText('Firmato');
});
