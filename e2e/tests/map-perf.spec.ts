import { expect, test } from '@playwright/test';

import { addPierThroughApi, canvas, createMap, marinaOf } from '../support/map';
import { seedPerfOccupancy, setupPrices } from '../support/occupancy';
import { measureFrames, openOps, opsSvg, panFor } from '../support/opsmap';
import { login } from '../support/session';

// Rendering budget (m3-plan §9.5, D26), gated by PORTO_E2E_PERF=1 (CI job e2e-perf). Blocking from M3 on the
// operational view: 3 040 berths with 2 000 occupied, avg frame ≤ 20 ms and p95 ≤ 34 ms over three pans
// (overview, 4× zoom with silhouettes, after a lens switch). A 4× CPU-throttled run is logged only.
test.skip(
  process.env.PORTO_E2E_PERF !== '1',
  'set PORTO_E2E_PERF=1 to run the map performance test',
);

const AVG_MS = 20;
const P95_MS = 34;

async function perfHarbour(page: Parameters<typeof login>[0]) {
  await login(page);
  const marinaId = marinaOf(page);
  const map = await createMap(page, marinaId, `Perf ${Date.now()}`, 220_000, 100_000);
  for (let i = 0; i < 8; i++) {
    await addPierThroughApi(page, marinaId, map.id, {
      code: String.fromCharCode(65 + i),
      origin: { x: 10_000 + i * 25_000, y: 90_000 },
      berthsPerSide: 190,
      keyPrefix: `p${i}-`,
    });
  }
  return { marinaId, mapId: map.id };
}

test('operational view: 3 040 berths, 2 000 occupied, pans within the frame budget', async ({
  page,
}) => {
  test.setTimeout(15 * 60_000);
  const { marinaId, mapId } = await perfHarbour(page);
  // Transits are priced on creation (no_price otherwise); the perf job runs on its own server. A retry (CI) runs on
  // the same server, where the price lists of the first attempt are still active: creating them again is a 409.
  if (test.info().retry === 0) await setupPrices(page, marinaId);
  await seedPerfOccupancy(page, marinaId, mapId, 2000);
  await openOps(page, marinaId, mapId, { lens: 'occupancy', mode: 'scale' });
  await expect(page.getByTestId('ops-legend-count-transit')).toHaveText('2000', {
    timeout: 60_000,
  });
  const box = (await opsSvg(page).boundingBox())!;
  const centre = { x: box.x + box.width / 2, y: box.y + box.height / 2 };

  const runs: Record<string, { frames: number; avg: number; p95: number }> = {};
  runs.overview = await measureFrames(page, () => panFor(page, 3000, centre));
  await page.mouse.move(centre.x, centre.y);
  for (let i = 0; i < 4; i++) await page.mouse.wheel(0, -350); // ≈ 4× zoom, silhouettes visible
  runs.zoom4x = await measureFrames(page, () => panFor(page, 3000, centre));
  await page.getByTestId('ops-lens-tab-presence').click();
  await expect(page.getByTestId('ops-map')).toHaveAttribute('data-lens', 'presence');
  runs.lensPresence = await measureFrames(page, () => panFor(page, 3000, centre));

  // Tablet approximation (informational).
  const cdp = await page.context().newCDPSession(page);
  await cdp.send('Emulation.setCPUThrottlingRate', { rate: 4 });
  const tablet = await measureFrames(page, () => panFor(page, 3000, centre));
  await cdp.send('Emulation.setCPUThrottlingRate', { rate: 1 });

  for (const [name, r] of Object.entries(runs)) {
    test.info().annotations.push({ type: `${name}_frames`, description: String(r.frames) });
    test.info().annotations.push({ type: `${name}_avg_ms`, description: r.avg.toFixed(1) });
    test.info().annotations.push({ type: `${name}_p95_ms`, description: r.p95.toFixed(1) });
  }
  test.info().annotations.push({ type: 'tablet_4x_avg_ms', description: tablet.avg.toFixed(1) });
  test.info().annotations.push({ type: 'tablet_4x_p95_ms', description: tablet.p95.toFixed(1) });
  for (const [name, r] of Object.entries(runs)) {
    expect(r.frames, `${name} frames`).toBeGreaterThan(0);
    expect(r.avg, `${name} avg frame (ms)`).toBeLessThanOrEqual(AVG_MS);
    expect(r.p95, `${name} p95 frame (ms)`).toBeLessThanOrEqual(P95_MS);
  }
});

test('editor: pans a 3 000-berth map and logs frame times (informational, M1)', async ({
  page,
}) => {
  test.setTimeout(5 * 60_000);
  const { marinaId, mapId } = await perfHarbour(page);
  await page.goto(`/admin/m/${marinaId}/maps/${mapId}/edit`);
  await expect(page.getByTestId('berth-count')).toHaveText('Posti: 3040', { timeout: 60_000 });
  await page.getByRole('radio', { name: 'Sposta la vista (H)' }).click();
  const box = (await canvas(page).boundingBox())!;
  const r = await measureFrames(page, () =>
    panFor(page, 3000, { x: box.x + box.width / 2, y: box.y + box.height / 2 }),
  );
  test.info().annotations.push({ type: 'frames', description: String(r.frames) });
  test.info().annotations.push({ type: 'frame_avg_ms', description: r.avg.toFixed(1) });
  test.info().annotations.push({ type: 'frame_p95_ms', description: r.p95.toFixed(1) });
  expect(r.frames).toBeGreaterThan(0);
});
