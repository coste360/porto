import { expect, test } from '@playwright/test';

import { adminRequest, marinaApi } from '../support/map';
import { setupOpsHarbour, setupPrices, archiveOpsHarbour } from '../support/occupancy';
import { createCustomer, createVessel, dayFromToday, uniq } from '../support/registry';
import { login } from '../support/session';

// Quotes with option (13.2, m3-plan §9.5): two alternatives holding their berths, accept the second → transit,
// the first hold is released.

let harbour: string | null = null;
test.afterEach(async ({ page }) => {
  if (harbour) await archiveOpsHarbour(page, harbour);
  harbour = null;
});

test('quote with two held options, accept option 2', async ({ page }) => {
  test.setTimeout(3 * 60_000);
  await login(page);
  const h = await setupOpsHarbour(page, { berths: 4 });
  harbour = h.marinaId;
  await setupPrices(page, h.marinaId);
  await page.goto(`/admin/m/${h.marinaId}`);
  const tag = uniq();
  const customer = await createCustomer(page, {
    kind: 'person',
    first_name: 'Rita',
    last_name: `Prev ${tag}`,
  });
  const vessel = await createVessel(page, {
    name: `Grecale ${tag}`,
    loa_cm: 1000,
    beam_cm: 350,
  });
  const [b0, b1] = h.berths;
  const from = dayFromToday(10);
  const to = dayFromToday(13);
  await page.goto(
    `/admin/m/${h.marinaId}/quotes/new?vessel=${vessel.id}&rt=berth&rid=${b0!.id}&from=${from}&to=${to}`,
  );

  const editor = page.getByTestId('desk-quote-editor');
  await editor.getByRole('combobox', { name: 'Cliente' }).click();
  await page.getByRole('option', { name: customer.display_name }).click();
  const options = editor.getByTestId('desk-quote-option');
  await options.nth(0).getByRole('switch').click();
  await editor.getByRole('button', { name: "Aggiungi un'alternativa" }).click();
  await options.nth(1).getByRole('combobox', { name: 'Posto o risorsa' }).click();
  await page.getByRole('option', { name: new RegExp(`^${b1!.code} ·`) }).click();
  await options.nth(1).getByRole('switch').click();
  await editor.getByRole('button', { name: 'Crea il preventivo' }).click();

  await expect(page.getByRole('heading', { name: /PRV-\d{4}-\d{4}/ })).toBeVisible();
  const cards = page.getByTestId('desk-quote-option-card');
  await expect(cards).toHaveCount(2);
  await expect(cards.nth(0)).toContainText('Posto in opzione');
  await expect(cards.nth(1)).toContainText('Posto in opzione');
  await expect(page.getByTestId('desk-quote-expiry')).toContainText('Scade tra');
  const quoteId = page.url().split('/').pop()!;

  await page.getByRole('button', { name: 'Segna come inviato' }).click();
  await expect(page.getByText('Inviato', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Accetta' }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByRole('radio', { name: /Alternativa 2/ }).check();
  await dialog.getByRole('button', { name: 'Accetta' }).click();
  await expect(page).toHaveURL(/\/transits\//);
  await expect(page.getByText('In opzione', { exact: true })).toBeVisible();

  const { data: q } = await adminRequest<{
    status: string;
    options: { seq: number; placements: { allocation_id?: string }[] }[];
  }>(page, 'GET', `${marinaApi(h.marinaId)}/quotes/${quoteId}`);
  expect(q.status).toBe('accepted');
  expect(q.options.find((o) => o.seq === 1)!.placements.every((p) => !p.allocation_id)).toBe(true);
});
