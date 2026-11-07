import { expect, test } from '@playwright/test';

import { BERTH_VESSEL, createCustomerWithEmail, uniqTag } from '../support/contractsM4';
import { adminRequest, marinaApi } from '../support/map';
import {
  archiveOpsHarbour,
  createVesselViaApi,
  marinaDay,
  setupOpsHarbour,
  setupPrices,
} from '../support/occupancy';
import { login } from '../support/session';

// Charter (m4-plan §9.5): fleet, charter contract on two berths, rotation from a night (drag a vessel onto the
// other berth: the two vessels swap), never repriced.

let harbour: string | null = null;
test.afterEach(async ({ page }) => {
  if (harbour) await archiveOpsHarbour(page, harbour);
  harbour = null;
});

test('fleet → charter → rotation drag', async ({ page }) => {
  test.setTimeout(4 * 60_000);
  await login(page);
  const h = await setupOpsHarbour(page, { berths: 4 });
  harbour = h.marinaId;
  await setupPrices(page, h.marinaId);
  const company = await createCustomerWithEmail(
    page,
    h.marinaId,
    `charter-${uniqTag()}@e2e.porto.test`,
    'Charter',
  );
  const v1 = await createVesselViaApi(page, h.marinaId, { ...BERTH_VESSEL, name: 'Flotta Uno' });
  const v2 = await createVesselViaApi(page, h.marinaId, { ...BERTH_VESSEL, name: 'Flotta Due' });
  const api = marinaApi(h.marinaId);
  const fleet = await adminRequest<{ id: string }>(page, 'POST', `${api}/charter-fleets`, {
    body: {
      customer_id: company.id,
      name: 'Flotta E2E',
      vessel_ids: [v1.id, v2.id],
    },
  });
  const startsOn = marinaDay(0);
  const endsOn = marinaDay(120);
  const charter = await adminRequest<{ id: string; total_cents: number }>(
    page,
    'POST',
    `${api}/charters`,
    {
      body: {
        fleet_id: fleet.data.id,
        starts_on: startsOn,
        ends_on: endsOn,
        pricing_mode: 'per_berth',
        changeover_weekday: 6,
        placements: [
          {
            target: { resource_type: 'berth', resource_id: h.berths[0]!.id },
            vessel_id: v1.id,
          },
          {
            target: { resource_type: 'berth', resource_id: h.berths[1]!.id },
            vessel_id: v2.id,
          },
        ],
        activate: true,
      },
    },
  );

  await page.goto(`/admin/m/${h.marinaId}/charters`);
  await expect(page.getByText('Flotta E2E')).toBeVisible();
  await page.getByRole('button', { name: 'Rotazione' }).click();
  const board = page.getByRole('dialog');
  const lines = board.getByTestId('rotation-line');
  await expect(lines).toHaveCount(2);
  await board.getByText('Flotta Due', { exact: true }).first().dragTo(lines.nth(0));
  await expect(board.getByText('Cambiata')).toHaveCount(2);
  await board.getByRole('button', { name: 'Applica la rotazione' }).click();
  await expect(board).toBeHidden();
  const after = await adminRequest<{
    total_cents: number;
    allocations: {
      vessel_id?: string;
      resource_id: string;
      status: string;
      last_night: string;
    }[];
  }>(page, 'GET', `${api}/contracts/${charter.data.id}`);
  expect(after.data.total_cents).toBe(charter.data.total_cents);
  const live = after.data.allocations.filter(
    (a) => a.status === 'active' && a.last_night >= startsOn,
  );
  expect(live.some((a) => a.resource_id === h.berths[0]!.id && a.vessel_id === v2.id)).toBe(true);
});
