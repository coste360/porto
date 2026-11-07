import { expect, test } from '@playwright/test';

import {
  createTransitViaApi,
  setupOpsHarbour,
  setupPrices,
  archiveOpsHarbour,
} from '../support/occupancy';
import { today } from '../support/opsmap';
import { createVessel, dayFromToday, uniq } from '../support/registry';
import { login } from '../support/session';

// Presences (9.5, m3-plan §9.5): a confirmed transit arriving today is in the expected arrivals; one tap records
// the arrival with a note and a photo; the transit becomes "Arrivato" and the presence is on its page.

const PNG = Buffer.from(
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==',
  'base64',
);

let harbour: string | null = null;
test.afterEach(async ({ page }) => {
  if (harbour) await archiveOpsHarbour(page, harbour);
  harbour = null;
});

test('expected arrival → Arrivato with note and photo → transit arrived', async ({ page }) => {
  test.setTimeout(3 * 60_000);
  await login(page);
  const h = await setupOpsHarbour(page, { berths: 2 });
  harbour = h.marinaId;
  await setupPrices(page, h.marinaId);
  await page.goto(`/admin/m/${h.marinaId}`);
  const tag = uniq();
  const vessel = await createVessel(page, {
    name: `Bora ${tag}`,
    loa_cm: 900,
    beam_cm: 320,
  });
  const tr = await createTransitViaApi(page, h.marinaId, {
    vesselId: vessel.id,
    berthId: h.berths[0]!.id,
    arrival: today(),
    departure: dayFromToday(2),
    confirm: true,
  });

  await page.goto(`/admin/m/${h.marinaId}/dock`);
  const arrivals = page.getByTestId('desk-movements-arrivals_expected');
  await expect(arrivals).toContainText(`Bora ${tag}`);
  await arrivals.getByRole('button', { name: 'Arrivato' }).click();
  const dialog = page.getByTestId('desk-presence-dialog');
  await dialog.getByLabel('Note').fill('Ormeggiato di poppa');
  await dialog
    .getByLabel('Foto')
    .setInputFiles({ name: 'arrivo.png', mimeType: 'image/png', buffer: PNG });
  await dialog.getByRole('button', { name: "Registra l'arrivo" }).click();
  await expect(dialog).toBeHidden();
  await expect(page.getByTestId('dock-page')).toContainText('Arrivate: 1');

  await page.goto(`/admin/m/${h.marinaId}/transits/${tr.id}`);
  await expect(page.getByText('Arrivato', { exact: true }).first()).toBeVisible();
  await expect(page.getByText('Ormeggiato di poppa')).toBeVisible();
});
