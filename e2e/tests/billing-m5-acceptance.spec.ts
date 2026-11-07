import { readFileSync } from 'node:fs';

import { expect, test } from '@playwright/test';

import { marinaIdOf } from '../support/api';
import {
  billingDay,
  billingRequest,
  createBillingCustomer,
  createBillingItem,
  createManualCharge,
  recordPayment,
} from '../support/billing';
import { login } from '../support/session';

// M5 acceptance on the real binary without network (spec 32.3 M5, m5-plan §11.5): the charges of a month for a
// customer → export "da fatturare" (each charge exactly once, VAT totals per rate equal the charges) → the
// reconciliation CSV of the invoicing software → the statement of account closes at the customer's balance.

type Charge = { id: string; number: string; total_cents: number; vat_cents: number };
type Export = {
  id: string;
  number: string;
  charge_count: number;
  total_cents: number;
  totals_by_vat?: { vat_rate_bp: number; vat_cents: number; gross_cents: number }[];
};

test('month of charges → export → reconcile → statement', async ({ page }) => {
  test.setTimeout(3 * 60_000);
  await login(page);
  const marinaId = marinaIdOf(page);
  const m = `/api/admin/v1/marinas/${marinaId}`;
  const item = await createBillingItem(page.request, marinaId);
  const customer = await createBillingCustomer(page.request, marinaId);
  const ids: string[] = [];
  for (const [qty, unit, due] of [
    [1000, 12_200, -20],
    [2000, 6_100, -5],
    [3000, 1_000, 10],
    [1500, 3_333, 25],
  ] as const)
    ids.push(await createManualCharge(page.request, marinaId, customer.id, item.id, qty, unit, billingDay(due)));
  const charges = await Promise.all(
    ids.map((id) => billingRequest<Charge>(page.request, 'GET', `${m}/charges/${id}`)),
  );
  const expectedTotal = charges.reduce((s, c) => s + c.total_cents, 0);
  const expectedVat = charges.reduce((s, c) => s + c.vat_cents, 0);
  await recordPayment(page.request, marinaId, customer.id, charges[0]!.total_cents, 'bank_transfer', [
    ids[0]!,
  ]);

  // Export of this customer's month through the API (the wizard has its own spec), then the UI detail.
  const today = billingDay();
  const preview = await billingRequest<{ count: number; total_cents: number }>(
    page.request,
    'POST',
    `${m}/charge-exports/preview`,
    { body: { period_from: `${today.slice(0, 8)}01`, period_to: today, customer_id: customer.id } },
  );
  expect(preview.count).toBe(4);
  expect(preview.total_cents).toBe(expectedTotal);
  const exportId = await billingRequest<{ id: string }>(page.request, 'POST', `${m}/charge-exports`, {
    body: { period_from: `${today.slice(0, 8)}01`, period_to: today, customer_id: customer.id },
  }).then((e) => e.id);
  const exp = await billingRequest<{ export: Export }>(page.request, 'GET', `${m}/charge-exports/${exportId}`);
  expect(exp.export.charge_count).toBe(4);
  expect(exp.export.total_cents).toBe(expectedTotal);
  expect((exp.export.totals_by_vat ?? []).reduce((s, b) => s + b.vat_cents, 0)).toBe(expectedVat);
  // Nothing left: a second export of the same filters has no charge (INV-53).
  const again = await billingRequest<{ count: number }>(page.request, 'POST', `${m}/charge-exports/preview`, {
    body: { period_from: `${today.slice(0, 8)}01`, period_to: today, customer_id: customer.id },
  });
  expect(again.count).toBe(0);

  await page.goto(`/admin/m/${marinaId}/exports/${exportId}`);
  await expect(page.getByTestId('export-item')).toHaveCount(4);
  const download = page.waitForEvent('download');
  await page.getByRole('button', { name: 'Scarica CSV' }).click();
  const csv = readFileSync(await (await download).path(), 'utf8');
  for (const c of charges) expect(csv).toContain(c.number);

  const [d, mo, y] = [today.slice(8, 10), today.slice(5, 7), today.slice(0, 4)];
  const rows = charges.map((c, i) => `${c.number};FT-ACC-${i + 1};${d}/${mo}/${y}`).join('\n');
  await page.goto(`/admin/m/${marinaId}/exports/reconcile`);
  await page.getByLabel('Oppure incolla il contenuto').fill(`numero addebito;numero fattura;data\n${rows}\n`);
  await page.getByRole('button', { name: 'Prova' }).click();
  await expect(page.getByTestId('reconcile-summary')).toContainText('abbinate: 4');
  await page.getByRole('button', { name: 'Applica' }).click();
  await expect(page.getByText('Riconciliazione applicata')).toBeVisible();

  const balance = expectedTotal - charges[0]!.total_cents;
  const dash = await billingRequest<{ balance_cents: number }>(
    page.request,
    'GET',
    `${m}/customers/${customer.id}/dashboard`,
  );
  expect(dash.balance_cents).toBe(balance);
  await page.goto(`/admin/m/${marinaId}/customers/${customer.id}`);
  await page.getByRole('tab', { name: 'Estratto conto' }).click();
  await expect(page.getByTestId('statement-row')).toHaveCount(5);
  const closing = await page.getByTestId('statement-closing').innerText();
  const shown = Number(closing.replace(/[^\d,-]/g, '').replace(',', ''));
  expect(shown).toBe(balance);
});
