import { expect, test } from '@playwright/test';

import {
  createContractViaApi,
  createTransitViaApi,
  setupOpsHarbour,
  setupPrices,
  archiveOpsHarbour,
} from '../support/occupancy';
import { berthOnScreen, dragOnMap, lensFill, openOps, today } from '../support/opsmap';
import { createCustomer, createVessel, dayFromToday, uniq } from '../support/registry';
import { login } from '../support/session';
import { zoomAround, berthCentre } from '../support/map';

// Drag & drop on fixed berths (m3-plan §8.5, §9.5): berth → berth with "Da quando", red ghost on a conflict,
// undo from the toast.

// Archive the harbour of each run, otherwise later logins land on the marina picker.
let harbour: string | null = null;
test.afterEach(async ({ page }) => {
  if (harbour) await archiveOpsHarbour(page, harbour);
  harbour = null;
});

test('moves a transit by drag & drop, shows conflicts in red and undoes the move', async ({
  page,
}) => {
  test.setTimeout(2 * 60_000);
  await login(page);
  const h = await setupOpsHarbour(page, { berths: 4 });
  harbour = h.marinaId;
  // Vessels and customers are created in the marina of the page URL: enter the new marina first.
  await page.goto(`/admin/m/${h.marinaId}`);
  await setupPrices(page, h.marinaId);
  const tag = uniq();
  const owner = await createCustomer(page, {
    kind: 'person',
    first_name: 'Bea',
    last_name: `Drag ${tag}`,
  });
  const moving = await createVessel(page, {
    name: `Grecale ${tag}`,
    loa_cm: 900,
    beam_cm: 320,
  });
  const staying = await createVessel(page, {
    name: `Ponente ${tag}`,
    loa_cm: 900,
    beam_cm: 320,
  });
  const [b0, b1, b2] = h.berths;
  await createTransitViaApi(page, h.marinaId, {
    vesselId: moving.id,
    berthId: b0!.id,
    arrival: today(),
    departure: dayFromToday(4),
    confirm: true,
  });
  await createContractViaApi(page, h.marinaId, {
    vesselId: staying.id,
    customerId: owner.id,
    berthId: b1!.id,
    term: 'annual',
    startsOn: today(),
    activate: true,
  });
  await openOps(page, h.marinaId, h.mapId);
  await zoomAround(page, berthCentre(b1!), 4);

  // Over the contract berth the ghost is red with the conflicting vessel.
  await dragOnMap(page, await berthOnScreen(page, b0!), await berthOnScreen(page, b1!), {
    release: false,
  });
  await expect(page.getByTestId('ops-drag-ghost')).toHaveAttribute('data-state', 'conflict');
  await expect(page.getByTestId('ops-drag-conflict')).toContainText(`Ponente ${tag}`);
  await page.keyboard.press('Escape');
  await page.mouse.up();

  // Over a free berth it is green; the drop asks "Da quando".
  await dragOnMap(page, await berthOnScreen(page, b0!), await berthOnScreen(page, b2!), {
    release: false,
  });
  await expect(page.getByTestId('ops-drag-ghost')).toHaveAttribute('data-state', /ok|warn/);
  await page.mouse.up();
  const dialog = page.getByTestId('ops-move-dialog');
  await expect(dialog).toBeVisible();
  await expect(dialog.getByLabel('Da oggi')).toBeChecked();
  await expect(dialog.getByTestId('ops-move-check')).toHaveAttribute('data-ok', 'true');
  await dialog.getByTestId('ops-move-confirm').click();
  await expect(lensFill(page, b2!.id)).toHaveAttribute('data-lens', 'transit');
  await expect(lensFill(page, b0!.id)).toHaveAttribute('data-lens', 'free');

  // Undo from the toast puts it back.
  await page.getByTestId('ops-undo').click();
  await expect(lensFill(page, b0!.id)).toHaveAttribute('data-lens', 'transit');
  await expect(lensFill(page, b2!.id)).toHaveAttribute('data-lens', 'free');
});
