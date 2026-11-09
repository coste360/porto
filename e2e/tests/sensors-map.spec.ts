import { expect, test } from '@playwright/test';

import { adminRequest } from '../support/map';
import {
  archiveOpsHarbour,
  createTransitViaApi,
  createVesselViaApi,
  marinaDay,
  setupOpsHarbour,
  setupPrices,
} from '../support/occupancy';
import { openOps } from '../support/opsmap';
import { expectNoSeriousViolations, uniq } from '../support/registry';
import { login } from '../support/session';
import { postSignedReadings, readingPair } from '../support/sensors';

// Sensori live (m12-plan §9.2, §17.4): a confirmed transit arrives today on a berth with a sensor; a signed reading
// pair posted from the test turns the berth occupied on the Sensori lens within 2 s without a reload (SSE
// sensor.changed); the reconciliation proposes the transit arrival, which appears in the inbox and on the dock phone
// at 390 px, where the arrival is confirmed.

let harbour: string | null = null;
test.afterEach(async ({ page }) => {
  if (harbour) await archiveOpsHarbour(page, harbour);
  harbour = null;
});

test('sensors map: live occupied berth, arrival proposal confirmed on the dock phone', async ({ page, request }) => {
  test.setTimeout(3 * 60_000);
  await login(page);
  const { marinaId, mapId, berths } = await setupOpsHarbour(page, { berths: 4 });
  harbour = marinaId;
  await setupPrices(page, marinaId);
  const vessel = await createVesselViaApi(page, marinaId, { loaCm: 1000, beamCm: 330 });
  const berth = berths[0]!;
  await createTransitViaApi(page, marinaId, {
    vesselId: vessel.id,
    berthId: berth.id,
    arrival: marinaDay(0),
    departure: marinaDay(2),
    confirm: true,
  });
  const ext = `EXT-${uniq()}`;
  const created = await adminRequest<{ key: string }>(
    page,
    'POST',
    `/api/admin/v1/marinas/${marinaId}/sensors/devices`,
    { body: { external_id: ext, code: `S${uniq()}`, kind: 'radar', resource_type: 'berth', resource_id: berth.id } },
  );

  await openOps(page, marinaId, mapId);
  await page.getByTestId('ops-sensors-lens').click();
  await expect(page.getByTestId('sensors-legend')).toBeVisible();
  await expect(page.getByTestId('sensors-legend-count-occupied')).toHaveText('0');
  const res = await postSignedReadings(request, created.data.key, readingPair(ext, 'occupied'));
  expect(res.accepted).toBe(2);
  // Live, without a reload: the SSE stream invalidates the Sensori context.
  await expect(page.getByTestId('sensors-legend-count-occupied')).toHaveText('1', { timeout: 2_000 });

  // The reconciliation (woken by the ingestion) proposes the arrival: inbox, then the dock phone at 390 px.
  await page.goto(`/admin/m/${marinaId}/sensors`);
  await expect(page.getByTestId('alert-transit_arrival')).toBeVisible({ timeout: 70_000 });
  await expectNoSeriousViolations(page, 'sensors inbox');
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto(`/admin/m/${marinaId}/dock?tab=alerts`);
  const card = page.getByTestId('dock-alert-transit_arrival');
  await expect(card).toBeVisible();
  await card.getByTestId('alert-action-confirm_arrival').click();
  await expect(card).toBeHidden({ timeout: 10_000 });
  const contracts = await adminRequest<{ items: { status: string }[] }>(
    page,
    'GET',
    `/api/admin/v1/marinas/${marinaId}/sensors/alerts?status=confirmed`,
  );
  expect(contracts.data.items).toHaveLength(1);
});
