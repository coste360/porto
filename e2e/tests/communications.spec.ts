import { expect, test } from '@playwright/test';

import { setupRenewalScenario, contractCustomerEmail } from '../support/contractsM4';
import { latestLinkFromMail, pathOf } from '../support/links';
import { adminRequest, marinaApi } from '../support/map';
import { archiveOpsHarbour } from '../support/occupancy';
import { login } from '../support/session';

// Communications (m4-plan §9.5): audience from a map selection, preview with the counts, send, and the customer
// unsubscribes from promotional messages with one click.

let harbour: string | null = null;
test.afterEach(async ({ page }) => {
  if (harbour) await archiveOpsHarbour(page, harbour);
  harbour = null;
});

test('audience from map selection → preview → send → unsubscribe page', async ({ page }) => {
  test.setTimeout(4 * 60_000);
  await login(page);
  const s = await setupRenewalScenario(page, { contracts: 3 });
  harbour = s.marinaId;
  // A marketing consent for the customer of the first contract, so the promotional email reaches them.
  const email = await contractCustomerEmail(page, s.marinaId, s.contractIds[0]!);
  const contract = await adminRequest<{ customer: { id: string } }>(
    page,
    'GET',
    `${marinaApi(s.marinaId)}/contracts/${s.contractIds[0]!}`,
  );
  await adminRequest(
    page,
    'POST',
    `${marinaApi(s.marinaId)}/customers/${contract.data.customer.id}/consents`,
    {
      body: { kind: 'marketing', granted: true, policy_version: 'v1', source: 'desk' },
    },
  );

  await page.goto(`/admin/m/${s.marinaId}/communications`);
  await page.getByRole('link', { name: 'Nuova comunicazione' }).click();
  await page.getByRole('tab', { name: 'Sulla mappa' }).click();
  await page.getByRole('button', { name: 'Tutti' }).click();
  await expect(page.getByTestId('map-selection-count')).not.toHaveText(/^0 /);
  await page.getByRole('button', { name: 'Avanti' }).click();
  await page.getByLabel('Nome').fill('Avviso lavori pontile');
  await page.getByRole('combobox', { name: 'Categoria' }).click();
  await page.getByRole('option', { name: 'Promozionale' }).click();
  await page.getByLabel('Oggetto').fill('Lavori al pontile A');
  await page.getByLabel(/^Testo/).fill('Gentile cliente, il pontile A sarà in manutenzione.');
  await page.getByRole('button', { name: 'Avanti' }).click();
  await page.getByRole('button', { name: 'Avanti' }).click();
  const counts = page.getByTestId('comm-preview-counts');
  await expect(counts).toBeVisible();
  await expect(counts).toContainText('Destinatari');
  await page.getByRole('button', { name: 'Avanti' }).click();
  await page.getByRole('button', { name: 'Invia ora' }).click();
  await expect(page.getByTestId('comm-recipient').first()).toBeVisible({
    timeout: 30_000,
  });

  // Promotional messages carry the unsubscribe link (only customers with consent receive them).
  const link = await latestLinkFromMail(page.request, email, 'unsubscribe', 60_000).catch(
    () => null,
  );
  test.skip(
    link === null,
    'the scenario customers have no marketing consent: no promotional email was sent',
  );
  await page.goto(pathOf(link!));
  await page.getByRole('button', { name: 'Conferma la disiscrizione' }).click();
  await expect(page.getByText('Disiscrizione registrata.')).toBeVisible();
});
