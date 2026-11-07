import { expect, test } from '@playwright/test';

import {
  createContractViaApi,
  createTransitViaApi,
  setupOpsHarbour,
  setupPrices,
  archiveOpsHarbour,
} from '../support/occupancy';
import { today } from '../support/opsmap';
import {
  createCustomer,
  createVessel,
  dayFromToday,
  expectNoSeriousViolations,
  uniq,
} from '../support/registry';
import { login } from '../support/session';

// Phone panel (31.3, m3-plan §8.6, §9.5) at 390×844: arrival, unexpected arrival, release of an annual berth.

let harbour: string | null = null;
test.afterEach(async ({ page }) => {
  if (harbour) await archiveOpsHarbour(page, harbour);
  harbour = null;
});

test('dock panel on a phone: arrival, unexpected arrival, release', async ({ page }) => {
  test.setTimeout(3 * 60_000);
  await login(page);
  const h = await setupOpsHarbour(page, { berths: 6 });
  harbour = h.marinaId;
  await setupPrices(page, h.marinaId);
  await page.goto(`/admin/m/${h.marinaId}`);
  const tag = uniq();
  const owner = await createCustomer(page, {
    kind: 'person',
    first_name: 'Nino',
    last_name: `Dock ${tag}`,
  });
  const annual = await createVessel(page, {
    name: `Zefiro ${tag}`,
    loa_cm: 1000,
    beam_cm: 350,
  });
  const visitor = await createVessel(page, {
    name: `Ostro ${tag}`,
    loa_cm: 900,
    beam_cm: 320,
  });
  await createContractViaApi(page, h.marinaId, {
    vesselId: annual.id,
    customerId: owner.id,
    berthId: h.berths[0]!.id,
    term: 'annual',
    startsOn: today(),
    activate: true,
  });
  await createTransitViaApi(page, h.marinaId, {
    vesselId: visitor.id,
    berthId: h.berths[1]!.id,
    arrival: today(),
    departure: dayFromToday(1),
    confirm: true,
  });

  // Signed in on a desktop (the user menu is in the sidebar), then the phone viewport for the panel.
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto(`/admin/m/${h.marinaId}/dock`);
  const dock = page.getByTestId('dock-page');
  await expect(dock.getByRole('heading', { name: 'Banchina' })).toBeVisible();
  // No horizontal scroll at phone width; tabs are 44 px touch targets.
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);
  const tab = dock.getByRole('tab', { name: 'Oggi' });
  expect((await tab.boundingBox())!.height).toBeGreaterThanOrEqual(44);

  // Arrival of the expected transit.
  const arrivals = page.getByTestId('desk-movements-arrivals_expected');
  await arrivals
    .getByRole('listitem')
    .filter({ hasText: `Ostro ${tag}` })
    .getByRole('button', { name: 'Arrivato' })
    .click();
  await page
    .getByTestId('desk-presence-dialog')
    .getByRole('button', { name: "Registra l'arrivo" })
    .click();
  await expect(dock).toContainText('Arrivate: 1');

  // Unexpected arrival: new vessel with estimated dimensions on a free berth.
  await dock.getByRole('tab', { name: 'Arrivo' }).click();
  await page.getByLabel("Nome dell'imbarcazione").fill(`Libeccio ${tag}`);
  await page.getByLabel('Lunghezza fuori tutto').fill('8,5');
  await page.getByLabel('Larghezza', { exact: true }).fill('3');
  await page.getByRole('combobox', { name: 'Posto o risorsa' }).click();
  await page.getByRole('option', { name: new RegExp(`^${h.berths[2]!.code} ·`) }).click();
  await page.getByRole('button', { name: "Registra l'arrivo" }).click();
  await expect(page.getByText('Arrivo registrato')).toBeVisible();

  // Release of the annual berth for two nights, from the bottom sheet.
  await dock.getByRole('tab', { name: 'Rilasci' }).click();
  await page.getByLabel('Cerca il contratto per barca o codice').fill(`Zefiro ${tag}`);
  await page.getByRole('button', { name: new RegExp(`Zefiro ${tag}`) }).click();
  const sheet = page.getByTestId('desk-release-dialog').or(page.getByRole('dialog'));
  await expect(sheet.getByText('Rilascia notti')).toBeVisible();
  await sheet.getByRole('button', { name: 'Rilascia', exact: true }).click();
  await expect(sheet).toBeHidden();

  await expectNoSeriousViolations(page, 'dock light');
});
