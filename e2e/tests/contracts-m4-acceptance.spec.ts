import { expect, test, type Browser, type Page } from '@playwright/test';

import {
  BERTH_VESSEL,
  contractCustomerEmail,
  createCustomerWithEmail,
  createQuoteViaApi,
  setupRenewalScenario,
  uniqTag,
} from '../support/contractsM4';
import { latestLinkFromMail, latestOtpFromMail, pathOf } from '../support/links';
import { adminRequest, marinaApi } from '../support/map';
import { archiveOpsHarbour, createVesselViaApi, marinaDay } from '../support/occupancy';
import { expectNoSeriousViolations } from '../support/registry';
import { login } from '../support/session';

// M4 acceptance from the UI (m4-plan §9.5, spec 32.3 M4): quote → link → sign → active → settle → renew →
// campaign of 10 → sign → deposit; axe on every new page in light and dark.

let harbour: string | null = null;
test.afterEach(async ({ page }) => {
  if (harbour) await archiveOpsHarbour(page, harbour);
  harbour = null;
});

/** The customer signs a document from the signing link on a phone; returns when the success page shows. */
/** Signs through a signature link on a phone; returns the contract code shown as the deposit reference. */
async function signOnPhone(browser: Browser, link: string, email: string): Promise<string> {
  const ctx = await browser.newContext({
    viewport: { width: 390, height: 844 },
    locale: 'it-IT',
  });
  const p = await ctx.newPage();
  await p.goto(pathOf(link));
  await p.getByRole('checkbox', { name: /Confermo che sono io/ }).check();
  await p.getByRole('button', { name: 'Inviami il codice' }).click();
  await p.getByRole('textbox').fill(await latestOtpFromMail(ctx.request, email));
  await p.getByRole('button', { name: 'Firma', exact: true }).click();
  await expect(p.getByText('Documento firmato. Grazie!')).toBeVisible();
  await expect(p.getByTestId('deposit')).toBeVisible();
  const code = /Causale: (\S+)/.exec(await p.getByTestId('deposit').innerText())?.[1] ?? '';
  await ctx.close();
  return code;
}

/** Settles the issued first instalment (the deposit) of a contract from the scadenzario. */
async function settleDeposits(page: Page, marinaId: string, contractCode: string): Promise<void> {
  await page.goto(`/admin/m/${marinaId}/installments`);
  await page
    .getByRole('row')
    .filter({ hasText: contractCode })
    .first()
    .getByRole('checkbox')
    .check();
  await page.getByRole('button', { name: 'Registra incasso' }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByRole('combobox', { name: 'Metodo' }).click();
  await page.getByRole('option', { name: 'Bonifico' }).click();
  await dialog.getByLabel(/Riferimento/).fill(`CRO ${uniqTag()}`);
  await dialog.getByRole('button', { name: 'Registra' }).click();
  await expect(dialog).toBeHidden();
}

test('quote → link → sign → active → settle → renew → campaign of 10 → sign → deposit', async ({
  page,
  browser,
}) => {
  test.setTimeout(10 * 60_000);
  await login(page);
  // Ten active contracts for the campaign plus two free berths for the quote.
  const s = await setupRenewalScenario(page, { contracts: 10 });
  harbour = s.marinaId;
  const api = marinaApi(s.marinaId);
  const berths = await adminRequest<{ items: { id: string }[] }>(
    page,
    'GET',
    `${api}/berths?limit=200`,
  );
  const busy = new Set<string>();
  for (const id of s.contractIds) {
    const c = await adminRequest<{ allocations: { resource_id: string }[] }>(
      page,
      'GET',
      `${api}/contracts/${id}`,
    );
    c.data.allocations.forEach((a) => busy.add(a.resource_id));
  }
  const free = berths.data.items.find((b) => !busy.has(b.id));
  test.skip(!free, 'no free berth in the scenario harbour');

  // 1. Quote sent by email, accepted and signed by the customer.
  const email = `accettazione-${uniqTag()}@e2e.porto.test`;
  const customer = await createCustomerWithEmail(page, s.marinaId, email);
  const vessel = await createVesselViaApi(page, s.marinaId, BERTH_VESSEL);
  const quote = await createQuoteViaApi(page, s.marinaId, {
    customerId: customer.id,
    vesselId: vessel.id,
    berthId: free!.id,
    startsOn: marinaDay(5),
  });
  await page.goto(`/admin/m/${s.marinaId}/quotes/${quote.id}`);
  await page.getByRole('button', { name: 'Invia per email' }).click();
  await page.getByRole('dialog').getByRole('button', { name: 'Invia per email' }).click();
  await expect(page.getByText('Preventivo inviato al cliente.')).toBeVisible();
  const qctx = await browser.newContext({
    viewport: { width: 390, height: 844 },
    locale: 'it-IT',
  });
  const q = await qctx.newPage();
  await q.goto(pathOf(await latestLinkFromMail(page.request, email, 'quote')));
  const reg = q.getByLabel('Matricola');
  if (await reg.count()) {
    await reg.fill('GE8001');
    await q
      .getByLabel("Scadenza dell'assicurazione")
      .fill(`${Number(marinaDay(0).slice(0, 4)) + 2}-06-30`);
  }
  await q.getByRole('button', { name: 'Accetto il preventivo' }).click();
  await expect(q.getByText(/Preventivo accettato: contratto/)).toBeVisible();
  await qctx.close();
  // The signature link arrives by email (no signature_link in the acceptance answer yet).
  const quoted = await signOnPhone(
    browser,
    await latestLinkFromMail(page.request, email, 'signature'),
    email,
  );

  // 2. Active; the office settles the deposit.
  await page.reload();
  await page.getByRole('link', { name: 'Apri il contratto' }).click();
  await expect(page.getByText('Attivo', { exact: true })).toBeVisible();
  const contractUrl = page.url();
  await settleDeposits(page, s.marinaId, quoted);

  // 3. Simple renewal of that contract from its page.
  await page.goto(contractUrl);
  await page.getByRole('tab', { name: 'Rinnovo' }).click();
  await page.getByRole('button', { name: 'Rinnova' }).click();
  await page.getByRole('dialog').getByRole('button', { name: 'Rinnova' }).click();
  await expect(page.getByText('Proposta di rinnovo')).toBeVisible();

  // 4. Campaign over the ten contracts; one customer signs and pays the deposit.
  await page.goto(`/admin/m/${s.marinaId}/renewals/new`);
  await page.getByLabel('Nome della campagna').fill('Accettazione M4');
  await page.getByRole('checkbox', { name: 'Annuale' }).check();
  await page.getByRole('button', { name: 'Calcola anteprima' }).click();
  await expect(page.getByTestId('proposal-row').first()).toBeVisible({
    timeout: 60_000,
  });
  await page.getByRole('button', { name: 'Avanti' }).click();
  await page.getByRole('button', { name: 'Avanti' }).click();
  await page.getByRole('button', { name: 'Avvia la campagna' }).click();
  await expect(page.getByTestId('count-sent')).toHaveText(/10/, {
    timeout: 90_000,
  });
  const renewalEmail = await contractCustomerEmail(page, s.marinaId, s.contractIds[1]!);
  const rctx = await browser.newContext({
    viewport: { width: 390, height: 844 },
    locale: 'it-IT',
  });
  const r = await rctx.newPage();
  await r.goto(pathOf(await latestLinkFromMail(page.request, renewalEmail, 'renewal')));
  await r.getByRole('button', { name: 'Accetto il rinnovo' }).click();
  await rctx.close();
  const renewed = await signOnPhone(
    browser,
    await latestLinkFromMail(page.request, renewalEmail, 'signature'),
    renewalEmail,
  );
  await expect(page.getByTestId('count-signed')).toHaveText(/1/);
  await settleDeposits(page, s.marinaId, renewed);
  await page.goto(`/admin/m/${s.marinaId}/renewals`);
  await page.getByRole('link', { name: 'Accettazione M4' }).click();
  await expect(page.getByTestId('count-completed')).toHaveText(/1/, {
    timeout: 10_000,
  });
});

const PAGES = [
  'installments',
  'renewals',
  'renewals/new',
  'charters',
  'waitlist',
  'settings/contracts',
  'rights',
  'transfers',
  'mandates',
  'fees',
  'common-charges',
  'holder-installments',
  'concession',
  'templates',
  'signatures',
  'communications',
  'communications/new',
  'imports',
  'settings/signature',
  'deadlines',
];

for (const scheme of ['light', 'dark'] as const) {
  test(`axe on every new M4 page (${scheme})`, async ({ page }) => {
    test.setTimeout(4 * 60_000);
    await page.emulateMedia({ colorScheme: scheme });
    await login(page);
    const shell = new URL(page.url()).pathname.replace(/\/$/, '');
    for (const p of PAGES) {
      await page.goto(`${shell}/${p}`);
      await expect(page.getByRole('main').first()).toBeVisible();
      await page.waitForLoadState('networkidle');
      await expectNoSeriousViolations(page, `${p}/${scheme}`);
    }
  });
}
