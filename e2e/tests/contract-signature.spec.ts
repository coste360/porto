import { expect, test } from '@playwright/test';

import { createDraftContract, uniqTag } from '../support/contractsM4';
import { adminRequest, marinaApi } from '../support/map';
import { archiveOpsHarbour, setupOpsHarbour, setupPrices } from '../support/occupancy';
import { login } from '../support/session';

// Paper signature (m4-plan §9.5, D11): request with the paper driver, scan upload, "firmato su carta" activates the
// contract; then an addendum on the active contract.

let harbour: string | null = null;
test.afterEach(async ({ page }) => {
  if (harbour) await archiveOpsHarbour(page, harbour);
  harbour = null;
});

test('paper scan upload + confirm activates the contract; addendum', async ({ page }) => {
  test.setTimeout(4 * 60_000);
  await login(page);
  const h = await setupOpsHarbour(page, { berths: 2 });
  harbour = h.marinaId;
  await setupPrices(page, h.marinaId);
  const contract = await createDraftContract(
    page,
    h.marinaId,
    h.berths[0]!.id,
    `carta-${uniqTag()}@e2e.porto.test`,
  );

  await page.goto(`/admin/m/${h.marinaId}/contracts/${contract.id}`);
  await page.getByRole('button', { name: 'Richiedi firma' }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByRole('combobox', { name: 'Modalità' }).click();
  await page.getByRole('option', { name: 'Firma su carta' }).click();
  await dialog.getByRole('button', { name: 'Invia' }).click();
  await expect(dialog).toBeHidden();

  await page.getByRole('tab', { name: 'Firma' }).click();
  const panel = page.getByTestId('signature-request');
  await expect(panel).toBeVisible({ timeout: 30_000 });
  await panel.getByLabel('Scansione del documento firmato').setInputFiles('fixtures/m4/scan.pdf');
  await expect(panel.getByText('Scansione caricata.')).toBeVisible();
  await panel.getByRole('button', { name: 'Conferma «firmato su carta»' }).click();
  await expect(page.getByText('Attivo', { exact: true })).toBeVisible({
    timeout: 15_000,
  });

  // The signed archive copy appears among the documents.
  await page.getByRole('tab', { name: 'Documenti' }).click();
  await expect(page.getByText('Firmato', { exact: true }).first()).toBeVisible();

  // Addendum: price change with a reason, activated on paper.
  await page.getByRole('tab', { name: 'Addenda' }).click();
  await page.getByRole('button', { name: 'Nuovo addendum' }).click();
  const add = page.getByRole('dialog');
  await add.getByRole('combobox', { name: 'Tipo' }).click();
  await page.getByRole('option', { name: 'Variazione di prezzo' }).click();
  await add.getByLabel('Nuovo totale').fill('3500,00');
  await add.getByLabel('Motivo').fill('Accordo con il cliente');
  await add.getByRole('button', { name: 'Crea addendum' }).click();
  await expect(add).toBeHidden();
  await page.getByRole('button', { name: 'Firmato su carta' }).click();
  await expect(page.getByText('Attivo', { exact: true }).last()).toBeVisible();
  const { data } = await adminRequest<{ items: { status: string }[] }>(
    page,
    'GET',
    `${marinaApi(h.marinaId)}/contracts/${contract.id}/addenda`,
  );
  expect(data.items[0]!.status).toBe('active');
});
