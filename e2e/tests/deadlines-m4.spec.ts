import { expect, test } from '@playwright/test';

import { setupRenewalScenario } from '../support/contractsM4';
import { archiveOpsHarbour } from '../support/occupancy';
import { login } from '../support/session';

// Deadline engine (m4-plan §9.5, §6.8): sync now creates the M4 kinds (contract end of contracts ending within
// the lead days), the kind filter lists them, the policy editor saves the channels.

let harbour: string | null = null;
test.afterEach(async ({ page }) => {
  if (harbour) await archiveOpsHarbour(page, harbour);
  harbour = null;
});

test('engine kinds, filters, sync now, policy channels', async ({ page }) => {
  test.setTimeout(4 * 60_000);
  await login(page);
  const s = await setupRenewalScenario(page, { contracts: 3 });
  harbour = s.marinaId;
  await page.goto(`/admin/m/${s.marinaId}/deadlines`);
  await page.getByRole('button', { name: 'Aggiorna ora' }).click();
  await expect(page.getByText(/Scadenze aggiornate:/)).toBeVisible();
  await page.getByRole('combobox', { name: 'Tipo' }).click();
  await page.getByRole('option', { name: 'Fine contratto' }).click();
  await expect(page.getByText('Fine contratto').nth(1)).toBeVisible();

  await page.getByRole('button', { name: 'Anticipi di avviso' }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByLabel('Fine contratto', { exact: true }).fill('60, 30');
  await dialog.locator('#policy-contract_end-office_email').check();
  await dialog.getByRole('button', { name: 'Salva' }).click();
  await expect(page.getByText('Anticipi salvati.')).toBeVisible();
});
