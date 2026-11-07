import { expect, test } from '@playwright/test';

import { setupOpsHarbour, setupPrices, archiveOpsHarbour } from '../support/occupancy';
import { openOps, opsMap, today } from '../support/opsmap';
import { pickDate } from '../support/pricing';
import { createCustomer, dayFromToday, uniq } from '../support/registry';
import { login } from '../support/session';

// Desk flow (m3-plan §9.5, WEB-DESK): incoming-vessel search → solutions highlighted on the map → new transit
// → manual-driver confirmation → the map shows the transit.

let harbour: string | null = null;
test.afterEach(async ({ page }) => {
  if (harbour) await archiveOpsHarbour(page, harbour);
  harbour = null;
});

test('incoming vessel search → transit → manual confirmation → map shows the transit', async ({
  page,
}) => {
  test.setTimeout(3 * 60_000);
  await login(page);
  const h = await setupOpsHarbour(page, { berths: 6 });
  harbour = h.marinaId;
  await setupPrices(page, h.marinaId);
  await openOps(page, h.marinaId, h.mapId);
  const tag = uniq();
  const customer = await createCustomer(page, {
    kind: 'person',
    first_name: 'Ugo',
    last_name: `Desk ${tag}`,
  });

  const panel = page.getByTestId('desk-incoming');
  await expect(panel.getByRole('heading', { name: 'Imbarcazione in ingresso' })).toBeVisible();
  await panel.getByLabel('Lunghezza fuori tutto').fill('11,34');
  await panel.getByLabel('Larghezza', { exact: true }).fill('3,80');
  await pickDate(page, panel.getByLabel('Partenza'), dayFromToday(3));
  await expect(panel.getByTestId('desk-search-nights')).toHaveText('Notti: 3');
  await panel.getByRole('button', { name: 'Cerca', exact: true }).click();
  const solutions = panel.getByTestId('desk-solution');
  await expect(solutions.first()).toBeVisible();
  // 11,40 m (rounded up) × 5,00 €/m × 3 nights = 171,00 € on every berth of the list.
  await expect(solutions.first()).toContainText('171,00 €');
  await solutions.first().hover();
  await expect(opsMap(page)).toBeVisible();

  await solutions.first().getByRole('button', { name: 'Nuovo transito' }).click();
  const wizard = page.getByTestId('desk-transit-dialog');
  await wizard.getByRole('radio', { name: 'Nuova' }).click();
  await wizard.getByLabel("Nome dell'imbarcazione").fill(`Libeccio ${tag}`);
  await wizard.getByLabel('Lunghezza fuori tutto').fill('11,34');
  await wizard.getByLabel('Larghezza', { exact: true }).fill('3,80');
  await wizard.getByRole('combobox', { name: /Cliente/ }).click();
  await page.getByRole('option', { name: customer.display_name }).click();
  await wizard.getByRole('button', { name: 'Continua' }).click();
  await expect(wizard.getByTestId('desk-transit-solution')).toBeVisible();
  await wizard.getByRole('button', { name: 'Continua' }).click();
  await expect(wizard.getByTestId('desk-transit-price')).toHaveText('171,00 €');
  await wizard.getByLabel('Conferma e registra pagamento manuale').check();
  await wizard.getByLabel('Riferimento del pagamento').fill('POS 0042');
  await wizard.getByRole('button', { name: 'Conferma il transito' }).click();
  await expect(wizard.getByText(/Transito TRN-\d{4}-\d{4} creato/)).toBeVisible();
  await expect(wizard.getByText('Confermato', { exact: true })).toBeVisible();
  await wizard.getByRole('button', { name: 'Chiudi' }).click();

  // The day view is invalidated (SSE and the mutation): the berth shows the transit.
  await expect(
    opsMap(page).getByRole('button', {
      name: new RegExp(`, Transito, Libeccio ${tag}`),
    }),
  ).toBeVisible();
  await expect(opsMap(page)).toHaveAttribute('data-date', today());
});
