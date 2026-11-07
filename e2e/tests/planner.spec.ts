import { expect, test } from '@playwright/test';

import { adminRequest, marinaApi } from '../support/map';
import {
  createTransitViaApi,
  setupOpsHarbour,
  setupPrices,
  archiveOpsHarbour,
} from '../support/occupancy';
import { today } from '../support/opsmap';
import { createVessel, dayFromToday, uniq } from '../support/registry';
import { login } from '../support/session';

// Berth calendar (9.7, m3-plan §9.5): dragging the right edge of a transit bar extends the stay.

const COL_PX = 32;

let harbour: string | null = null;
test.afterEach(async ({ page }) => {
  if (harbour) await archiveOpsHarbour(page, harbour);
  harbour = null;
});

test('planner: extend a transit by dragging its bar', async ({ page }) => {
  test.setTimeout(3 * 60_000);
  await login(page);
  const h = await setupOpsHarbour(page, { berths: 4 });
  harbour = h.marinaId;
  await setupPrices(page, h.marinaId);
  await page.goto(`/admin/m/${h.marinaId}`);
  const vessel = await createVessel(page, {
    name: `Maestrale ${uniq()}`,
    loa_cm: 900,
    beam_cm: 320,
  });
  const tr = await createTransitViaApi(page, h.marinaId, {
    vesselId: vessel.id,
    berthId: h.berths[0]!.id,
    arrival: today(),
    departure: dayFromToday(3),
    confirm: true,
  });

  await page.goto(`/admin/m/${h.marinaId}/planner?map=${h.mapId}&from=${today()}`);
  const bar = page.getByTestId('desk-planner-bar').first();
  await expect(bar).toBeVisible();
  const box = (await bar.locator('rect').first().boundingBox())!;
  const y = box.y + box.height / 2;
  await page.mouse.move(box.x + box.width - 3, y);
  await page.mouse.down();
  await page.mouse.move(box.x + box.width - 3 + COL_PX, y, { steps: 4 });
  await page.mouse.move(box.x + box.width - 3 + 2 * COL_PX, y, { steps: 4 });
  await page.mouse.up();

  const dialog = page.getByTestId('desk-planner-change');
  await expect(dialog).toBeVisible();
  await dialog.getByRole('button', { name: 'Conferma' }).click();
  await expect(dialog).toBeHidden();

  const { data } = await adminRequest<{ ends_on: string }>(
    page,
    'GET',
    `${marinaApi(h.marinaId)}/contracts/${tr.id}`,
  );
  expect(data.ends_on).toBe(dayFromToday(5));
  await expect(
    page.getByRole('button', {
      name: new RegExp(`al ${dayFromToday(4).split('-').reverse().join('/')}`),
    }),
  ).toBeVisible();
});
