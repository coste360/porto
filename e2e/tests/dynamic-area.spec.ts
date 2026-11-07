import { expect, test, type Page } from '@playwright/test';

import {
  createTransitViaApi,
  setupOpsHarbour,
  setupPrices,
  archiveOpsHarbour,
} from '../support/occupancy';
import { dragOnMap } from '../support/opsmap';
import { createVessel, dayFromToday, uniq } from '../support/registry';
import { BASE_URL } from '../support/env';
import { login } from '../support/session';

// Layout session of a dynamic area (m3-plan §8.5, §9.5): two vessels exchanged and saved in one transaction,
// Restore, a stale save from a second tab shows the reload notice, compaction preview.

async function vesselBox(page: Page, name: string) {
  const v = page
    .getByTestId('ops-map')
    .getByRole('button', { name: `Barca ${name}, trascina per spostarla` });
  // The map fits its viewport after each draft change: wait until the box stops moving.
  let prev = '';
  for (let i = 0; i < 20; i++) {
    const box = (await v.boundingBox())!;
    const cur = JSON.stringify(box);
    if (cur === prev) return box;
    prev = cur;
    await page.waitForTimeout(100);
  }
  return (await v.boundingBox())!;
}

/** Drags a vessel of the layout along the area axis by `dxCm` (positive: away from the area start). */
async function dragVesselBy(page: Page, name: string, dxCm: number) {
  const box = await vesselBox(page, name);
  const from = { x: box.x + box.width / 2, y: box.y + box.height / 2 };
  const pxPerCm = Number(
    await page
      .getByTestId('ops-map')
      .locator('svg[data-px-per-cm]')
      .first()
      .getAttribute('data-px-per-cm'),
  );
  await dragOnMap(page, from, { x: from.x + dxCm * pxPerCm, y: from.y });
}

/**
 * Exchange in the draft: take Alfa (at 1 m) out, drag Beta (at 12 m, stern-to, 3 m beam) to the start of the
 * area, then drop Alfa from "Da collocare" where Beta was. Positions are measured on the current viewport.
 */
async function exchange(page: Page, tag: string, alfaId: string) {
  await page.getByTestId(`ops-layout-remove-${alfaId}`).click();
  await expect(page.getByTestId('ops-unplaced-count')).toHaveText('1');
  const box = await vesselBox(page, `Beta ${tag}`);
  const beta = { x: box.x + box.width / 2, y: box.y + box.height / 2 };
  const pxPerCm = Number(
    await page
      .getByTestId('ops-map')
      .locator('svg[data-px-per-cm]')
      .first()
      .getAttribute('data-px-per-cm'),
  );
  await dragOnMap(page, beta, { x: beta.x - 1100 * pxPerCm, y: beta.y });
  const chip = (await page.getByTestId(`ops-to-place-${alfaId}`).boundingBox())!;
  await dragOnMap(page, { x: chip.x + 10, y: chip.y + 10 }, beta);
}

// Archive the harbour of each run, otherwise later logins land on the marina picker.
let harbour: string | null = null;
test.afterEach(async ({ page }) => {
  if (harbour) await archiveOpsHarbour(page, harbour);
  harbour = null;
});

test('layout session: exchange, restore, stale save, compaction', async ({ page, browser }) => {
  test.setTimeout(3 * 60_000);
  await login(page);
  const h = await setupOpsHarbour(page, { berths: 2, linearAreaM: 40 });
  harbour = h.marinaId;
  // Vessels and customers are created in the marina of the page URL: enter the new marina first.
  await page.goto(`/admin/m/${h.marinaId}`);
  await setupPrices(page, h.marinaId);
  const tag = uniq();
  const a = await createVessel(page, {
    name: `Alfa ${tag}`,
    loa_cm: 1000,
    beam_cm: 300,
  });
  const b = await createVessel(page, {
    name: `Beta ${tag}`,
    loa_cm: 1000,
    beam_cm: 300,
  });
  const stay = {
    arrival: dayFromToday(0),
    departure: dayFromToday(3),
    confirm: true,
  };
  const ta = await createTransitViaApi(page, h.marinaId, {
    vesselId: a.id,
    areaX: 100,
    ...stay,
  });
  await createTransitViaApi(page, h.marinaId, {
    vesselId: b.id,
    areaX: 1200,
    ...stay,
  });
  const url = `/admin/m/${h.marinaId}/ops/area/${h.areaId}?date=${dayFromToday(0)}`;
  await page.goto(url);
  await expect(page.getByTestId('ops-unplaced-count')).toHaveText('0');

  // Exchange: take Alfa out, move Beta to the start, put Alfa where Beta was.
  await page
    .getByTestId('ops-map')
    .getByRole('button', { name: `Barca Alfa ${tag}, trascina per spostarla` })
    .waitFor();
  await exchange(page, tag, ta.allocationId);
  await expect(page.getByTestId('ops-unplaced-count')).toHaveText('0');

  // Restore discards the draft, then redo it and save.
  await page.getByTestId('ops-layout-restore').click();
  await expect(page.getByTestId('ops-layout-save')).toBeDisabled();
  await exchange(page, tag, ta.allocationId);
  await expect(page.getByTestId('ops-unplaced-count')).toHaveText('0');
  await page.getByTestId('ops-layout-save').click();
  await expect(page.getByTestId('ops-layout-dirty')).toHaveCount(0);
  await expect(page.getByTestId('ops-undo')).toBeVisible();

  // Stale save: a second tab saves first, this tab's save shows the reload notice.
  // The second session reuses the owner's cookies: a fresh login would land on the marina picker (two marinas).
  const other = await browser.newContext({
    baseURL: BASE_URL,
    storageState: await page.context().storageState(),
  });
  const page2 = await other.newPage();
  await page2.goto(url);
  await expect(page2.getByTestId('ops-unplaced-count')).toHaveText('0');
  // Compaction preview (the layout is already compact), then each tab drafts a different move.
  await page.getByTestId('ops-layout-compact').click();
  await dragVesselBy(page, `Beta ${tag}`, 2000);
  await expect(page.getByTestId('ops-layout-save')).toBeEnabled();
  await dragVesselBy(page2, `Alfa ${tag}`, 2500);
  await page2.getByTestId('ops-layout-save').click();
  await expect(page2.getByTestId('ops-layout-dirty')).toHaveCount(0);
  await page.getByTestId('ops-layout-save').click();
  await expect(page.getByTestId('ops-layout-stale')).toBeVisible();
  await other.close();
});
