import { expect, test } from '@playwright/test';

import { addPierThroughApi, canvas, createMap, marinaOf } from '../support/map';
import { login } from '../support/session';

// Rendering budget with 3 000 elements (m1-plan §8.4). Informational in M1, blocking from the M3 acceptance.
test.skip(
  process.env.PORTO_E2E_PERF !== '1',
  'set PORTO_E2E_PERF=1 to run the map performance test',
);

test('pans a 3 000-berth map and logs frame times', async ({ page }) => {
  test.setTimeout(5 * 60_000);
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
  await page.goto(`/admin/m/${marinaId}/maps/${map.id}/edit`);
  await expect(page.getByTestId('berth-count')).toHaveText('Posti: 3040', {
    timeout: 60_000,
  });
  await page.getByRole('radio', { name: 'Sposta la vista (H)' }).click();
  const box = (await canvas(page).boundingBox())!;
  await page.evaluate(() => {
    const w = window as unknown as { __frames: number[] };
    w.__frames = [];
    let last = performance.now();
    const tick = (t: number) => {
      w.__frames.push(t - last);
      last = t;
      if (w.__frames.length < 2000) requestAnimationFrame(tick);
    };
    requestAnimationFrame(tick);
  });
  const cx = box.x + box.width / 2;
  const cy = box.y + box.height / 2;
  const until = Date.now() + 3000;
  await page.mouse.move(cx, cy);
  await page.mouse.down();
  let dir = 1;
  while (Date.now() < until) {
    await page.mouse.move(cx + dir * 200, cy, { steps: 20 });
    dir = -dir;
  }
  await page.mouse.up();
  const frames = await page.evaluate(() => (window as unknown as { __frames: number[] }).__frames);
  const sorted = [...frames].sort((a, b) => a - b);
  const p95 = sorted[Math.floor(sorted.length * 0.95)] ?? 0;
  const avg = frames.reduce((s, f) => s + f, 0) / Math.max(1, frames.length);
  test.info().annotations.push({ type: 'frames', description: String(frames.length) });
  test.info().annotations.push({ type: 'frame_avg_ms', description: avg.toFixed(1) });
  test.info().annotations.push({ type: 'frame_p95_ms', description: p95.toFixed(1) });
  expect(frames.length).toBeGreaterThan(0);
});
