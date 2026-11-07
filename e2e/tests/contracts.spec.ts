import { expect, test } from '@playwright/test';

import { setupOpsHarbour, setupPrices, archiveOpsHarbour } from '../support/occupancy';
import { today } from '../support/opsmap';
import { createCustomer, createVessel, uniq } from '../support/registry';
import { login } from '../support/session';

// Mooring contract wizard (m3-plan §9.5): annual contract as a draft holding its berth, activated ("firmato su
// carta"), then terminated from today.

let harbour: string | null = null;
test.afterEach(async ({ page }) => {
  if (harbour) await archiveOpsHarbour(page, harbour);
  harbour = null;
});

test('contract wizard → draft → activate → terminate', async ({ page }) => {
  test.setTimeout(3 * 60_000);
  await login(page);
  const h = await setupOpsHarbour(page, { berths: 4 });
  harbour = h.marinaId;
  await setupPrices(page, h.marinaId);
  await page.goto(`/admin/m/${h.marinaId}`);
  const tag = uniq();
  const customer = await createCustomer(page, {
    kind: 'person',
    first_name: 'Elio',
    last_name: `Orm ${tag}`,
  });
  const vessel = await createVessel(page, {
    name: `Ponente ${tag}`,
    loa_cm: 1000,
    beam_cm: 350,
  });
  await page.goto(`/admin/m/${h.marinaId}/contracts/new?vessel=${vessel.id}&from=${today()}`);

  const wizard = page.getByTestId('desk-contract-wizard');
  await wizard.getByRole('combobox', { name: 'Cliente' }).click();
  await page.getByRole('option', { name: customer.display_name }).click();
  // Annual: the end defaults to one year later.
  await expect(wizard.getByLabel('Fine')).not.toHaveText('');
  await wizard.getByRole('button', { name: 'Cerca posti liberi' }).click();
  await wizard.getByTestId('desk-solution').first().getByRole('button', { name: 'Scegli' }).click();
  await expect(wizard.getByTestId('desk-contract-picked')).toBeVisible();
  await wizard.getByRole('button', { name: 'Salva come bozza' }).click();

  await expect(page.getByRole('heading', { name: /ORM-\d{4}-\d{4}/ })).toBeVisible();
  await expect(page.getByText('Bozza', { exact: true })).toBeVisible();
  await expect(page.getByText(/Posto in opzione fino al/)).toBeVisible();
  await page.getByRole('button', { name: 'Attiva (firmato su carta)' }).click();
  await expect(page.getByText('Attivo', { exact: true })).toBeVisible();
  await expect(page.getByTestId('desk-contract-total')).not.toHaveText('0,00 €');

  await page.getByRole('button', { name: 'Cessa il contratto' }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByLabel('Motivo').fill('Barca venduta');
  await dialog.getByRole('button', { name: 'Cessa il contratto' }).click();
  await expect(page.getByText('Cessato', { exact: true })).toBeVisible();
});
