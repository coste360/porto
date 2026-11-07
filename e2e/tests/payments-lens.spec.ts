import { expect, test } from '@playwright/test';

import {
  archiveOpsHarbour,
  createTransitViaApi,
  createVesselViaApi,
  marinaDay,
  setupOpsHarbour,
  setupPrices,
} from '../support/occupancy';
import { login } from '../support/session';

// M5 Pagamenti lens (spec 9.2, m5-plan §4.5, §11.5): a confirmed transit paying at departure has a charge due
// within 15 days, so its berth shows "In scadenza"; free berths have no contract. The lens is informative only.

let harbour: string | null = null;
test.afterEach(async ({ page }) => {
  if (harbour) await archiveOpsHarbour(page, harbour);
  harbour = null;
});

test('payments lens: due soon transit, legend counts, other berths without contract', async ({
  page,
}) => {
  test.setTimeout(3 * 60_000);
  await login(page);
  const h = await setupOpsHarbour(page, { berths: 6 });
  harbour = h.marinaId;
  await setupPrices(page, h.marinaId);
  const vessel = await createVesselViaApi(page, h.marinaId, { name: 'Pagante', loaCm: 1000, beamCm: 330 });
  await createTransitViaApi(page, h.marinaId, {
    vesselId: vessel.id,
    berthId: h.berths[0]!.id,
    arrival: marinaDay(0),
    departure: marinaDay(3),
    confirm: true,
  });

  await page.goto(`/admin/m/${h.marinaId}/ops?map=${h.mapId}`);
  await page.getByTestId('ops-payments-lens').click();
  await expect(page.getByTestId('ops-payments-lens')).toHaveAttribute('aria-pressed', 'true');
  const legend = page.getByTestId('payments-legend');
  await expect(legend).toBeVisible();
  await expect(page.getByTestId('payments-legend-due_soon')).toContainText('1');
  await expect(page.getByTestId('payments-legend-none')).not.toContainText(/^0$/);
  await expect(legend).toContainText("l'uscita della barca non viene mai bloccata");

  // Switching to the rights lens turns the payments lens off.
  await page.getByTestId('ops-rights-lens').click();
  await expect(page.getByTestId('ops-payments-lens')).toHaveAttribute('aria-pressed', 'false');
  await expect(page.getByTestId('payments-legend')).toHaveCount(0);
});
