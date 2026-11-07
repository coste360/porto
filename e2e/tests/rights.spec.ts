import { expect, test } from '@playwright/test';

import { createCustomerWithEmail, uniqTag } from '../support/contractsM4';
import { adminRequest, marinaApi } from '../support/map';
import { archiveOpsHarbour, marinaDay } from '../support/occupancy';
import { setupRightsHarbour } from '../support/rights';
import { login } from '../support/session';

// Rights of use (m4-plan §9.5): wizard with holders and the live Σ‰, a management mandate on a right, a transfer.

let harbour: string | null = null;
test.afterEach(async ({ page }) => {
  if (harbour) await archiveOpsHarbour(page, harbour);
  harbour = null;
});

test('right wizard with holders → mandate → transfer', async ({ page }) => {
  test.setTimeout(5 * 60_000);
  await login(page);
  const h = await setupRightsHarbour(page, { berths: 8 });
  harbour = h.marinaId;
  const a = await createCustomerWithEmail(
    page,
    h.marinaId,
    `quota-a-${uniqTag()}@e2e.porto.test`,
    'QuotaA',
  );
  const b = await createCustomerWithEmail(
    page,
    h.marinaId,
    `quota-b-${uniqTag()}@e2e.porto.test`,
    'QuotaB',
  );

  await page.goto(`/admin/m/${h.marinaId}/rights`);
  await expect(page.getByTestId('right-row')).toHaveCount(h.rightIds.length);
  await page.getByRole('button', { name: "Nuovo diritto d'uso" }).click();
  const w = page.getByRole('dialog');
  await w.getByRole('combobox', { name: 'Posto o risorsa' }).click();
  await page.getByRole('option').nth(1).click();
  await w.getByRole('combobox', { name: 'Titolare 1' }).click();
  await page.getByRole('option', { name: a.display_name }).click();
  await w.getByRole('button', { name: 'Aggiungi titolare' }).click();
  await w.getByRole('combobox', { name: 'Titolare 2' }).click();
  await page.getByRole('option', { name: b.display_name }).click();
  await w.locator('#right-share-0').fill('600');
  await expect(w.getByTestId('holders-sum')).toHaveText(/Somma 600 ‰ \(mancano 400\)/);
  await w.locator('#right-share-1').fill('400');
  await expect(w.getByTestId('holders-sum')).toHaveText(/Somma 1000 ‰ \(mancano 0\)/);
  await w.getByRole('button', { name: 'Crea diritto' }).click();
  await expect(page.getByRole('heading', { name: /Diritto d'uso del posto/ })).toBeVisible();
  const rightUrl = page.url();

  // Mandate on one of the scenario rights (form through the API: the dates are pickers), then its page.
  const mandate = await adminRequest<{ id: string }>(
    page,
    'POST',
    `${marinaApi(h.marinaId)}/mandates`,
    {
      body: {
        right_id: h.rightIds[0],
        from_on: marinaDay(1),
        to_on: marinaDay(30),
        owner_share_bp: 7000,
        channels: ['transit'],
        settlement: 'credit',
      },
    },
  );
  await page.goto(`/admin/m/${h.marinaId}/mandates`);
  await page
    .getByRole('link')
    .filter({ hasText: /\d{2}\/\d{2}\/\d{4}/ })
    .first()
    .click();
  await expect(page).toHaveURL(new RegExp(`/mandates/${mandate.data.id}`));
  await expect(page.getByRole('heading', { name: 'Mandato di gestione' })).toBeVisible();
  await expect(page.getByText('70 %')).toBeVisible();

  // Transfer of the right to the second holder alone.
  await page.goto(rightUrl);
  await page.getByRole('link', { name: 'Cedi' }).click();
  const t = page.getByRole('dialog');
  await t.getByRole('combobox', { name: 'Titolare 1' }).click();
  await page.getByRole('option', { name: b.display_name }).click();
  await t.getByRole('button', { name: 'Crea cessione' }).click();
  await expect(page.getByRole('heading', { name: 'Cessione' })).toBeVisible();
  await page.getByRole('button', { name: 'Firmata su carta' }).click();
  await expect(page.getByText('Completata')).toBeVisible();
});
