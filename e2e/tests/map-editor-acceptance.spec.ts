import path from 'node:path';

import { expect, test } from '@playwright/test';

import {
  ActionCounter,
  berthCentre,
  canvas,
  clickMap,
  getMap,
  mapToScreen,
  marinaOf,
  zoomAround,
} from '../support/map';
import { login } from '../support/session';

// Spec 32.3 M1: a marina of 6 piers and 400 berths configured from an orthophoto in under 30 minutes.

const ORTHOPHOTO = path.resolve(import.meta.dirname, '..', 'fixtures', 'orthophoto.png');
const QUAY_Y = 40_000;
/** Berths per side of each pier: A–D 34 (68 berths), E–F 32 (64 berths) = 400. */
const PIERS = [
  { code: 'A', perSide: 34 },
  { code: 'B', perSide: 34 },
  { code: 'C', perSide: 34 },
  { code: 'D', perSide: 34 },
  { code: 'E', perSide: 32 },
  { code: 'F', perSide: 32 },
];

test('configures 6 piers and 400 berths from an orthophoto', async ({ page }) => {
  test.slow();
  test.setTimeout(Number(process.env.PORTO_E2E_ACCEPT_TIMEOUT ?? 30 * 60_000));
  const started = Date.now();
  const actions = new ActionCounter();

  // 1. Owner creates the map "Porto" (800 × 500 m).
  await login(page);
  const marinaId = marinaOf(page);
  await page.getByRole('link', { name: 'Mappe' }).click();
  await page.getByRole('button', { name: 'Nuova mappa' }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByLabel('Nome (italiano)').fill('Porto');
  await dialog.getByLabel('Larghezza').fill('800');
  await dialog.getByLabel('Altezza').fill('500');
  await dialog.getByRole('button', { name: 'Crea' }).click();
  actions.tick(5);
  await expect(page).toHaveURL(/\/maps\/[^/]+\/edit$/);
  const mapId = new URL(page.url()).pathname.split('/').at(-2)!;
  await expect(page.getByRole('toolbar', { name: "Strumenti dell'editor" })).toBeVisible();

  // 2. Upload the orthophoto.
  await page.getByRole('tab', { name: 'Sfondo' }).click();
  await page.getByTestId('background-file').setInputFiles(ORTHOPHOTO);
  actions.tick(2);
  await expect(page.getByRole('button', { name: 'Scegli due punti' })).toBeVisible();

  // 3. Calibrate on the two marked points (pixels 100,100 and 900,100 at the initial 10 cm/px), 100 m apart.
  await page.getByRole('button', { name: 'Scegli due punti' }).click();
  await clickMap(page, { x: 1000, y: 1000 }, actions);
  await clickMap(page, { x: 9000, y: 1000 }, actions);
  await page.getByLabel('Distanza reale').fill('100');
  await page.getByRole('button', { name: 'Applica la scala' }).click();
  actions.tick(3);
  await expect(page.getByText('Scala: 1 pixel = 12,5 cm')).toBeVisible();
  // The first slider of the background panel is the opacity (the shared Slider does not label its thumb).
  const opacity = page
    .getByRole('region', { name: 'Sfondo e georeferenziazione' })
    .getByRole('slider')
    .first();
  await opacity.focus();
  await page.keyboard.press('ArrowLeft');
  await page.keyboard.press('ArrowLeft');
  await expect(page.getByText('Opacità 50%')).toBeVisible();
  actions.tick();

  // 4. Draw the quay: 4 clicks + Enter.
  await page.getByRole('radio', { name: 'Banchina (Q)' }).click();
  for (const x of [2000, 27_000, 52_000, 78_000]) await clickMap(page, { x, y: QUAY_Y }, actions);
  await page.keyboard.press('Enter');
  actions.tick(2);
  await expect(page.getByTestId('editor-status')).toContainText('Strutture: 1');
  await page.getByRole('radio', { name: 'Seleziona (V)' }).click();

  // 5. Six piers with the generator, origin picked on the quay (edge snap gives the direction).
  for (const [i, pier] of PIERS.entries()) {
    await page.getByRole('button', { name: 'Generatori' }).click();
    await page.getByRole('menuitem', { name: 'Pontile con posti' }).click();
    const panel = page.getByRole('region', { name: 'Pontile con posti' });
    await panel.getByLabel('Codice').fill(pier.code);
    await panel.getByRole('button', { name: 'Scegli sulla mappa' }).click();
    await clickMap(page, { x: 6000 + i * 12_000, y: QUAY_Y }, actions);
    await expect(panel.getByTestId('gen-origin')).not.toHaveText('Nessun punto');
    await panel
      .getByLabel('Lunghezza', { exact: true })
      .fill(String((pier.perSide * 400 + 200) / 100));
    await panel.getByLabel('Larghezza', { exact: true }).fill('3');
    await panel.getByLabel('Posti per lato (0 = riempi)').fill(String(pier.perSide));
    await panel.getByLabel('Larghezza del posto').fill('4');
    await panel.getByLabel('Lunghezza del posto').fill('12');
    await panel.getByLabel('Prefisso').fill(`${pier.code}-`);
    await panel.getByRole('button', { name: 'Anteprima' }).click();
    await expect(panel.getByTestId('generator-preview')).toContainText(`${pier.perSide * 2} posti`);
    await expect(panel.getByTestId('generator-preview')).toContainText('Errori: 0');
    await panel.getByRole('button', { name: 'Inserisci' }).click();
    actions.tick(12);
  }
  await expect(page.getByTestId('berth-count')).toHaveText('Posti: 400');

  // 6. The validation panel shows no errors, then save.
  await page.getByRole('tab', { name: /Verifica/ }).click();
  await expect(page.getByTestId('validation-summary')).toContainText('Errori: 0', {
    timeout: 15_000,
  });
  await page.getByRole('button', { name: 'Salva' }).click();
  actions.tick(2);
  await expect(page.getByText(/Mappa salvata/)).toBeVisible();

  // 7–8. After a reload the editor and the API both see 400 berths and 6 piers.
  await page.reload();
  await expect(page.getByTestId('berth-count')).toHaveText('Posti: 400');
  const saved = await getMap(page, marinaId, mapId);
  expect(saved.berths).toHaveLength(400);
  expect(saved.structures.filter((s) => s.kind === 'pier')).toHaveLength(6);

  // 9. Elapsed time and user actions.
  const elapsed = Date.now() - started;
  test.info().annotations.push({ type: 'elapsed_ms', description: String(elapsed) });
  test.info().annotations.push({
    type: 'user_actions',
    description: String(actions.count),
  });
  expect(elapsed).toBeLessThan(30 * 60_000);

  // 10. Drag A-1 onto its neighbour with snapping disabled (Alt): the save is refused and nothing changes.
  const a1 = saved.berths.find((b) => b.code === 'A-1')!;
  const a3 = saved.berths.find((b) => b.code === 'A-3')!;
  const from = berthCentre(a1);
  const to = berthCentre(a3);
  await zoomAround(page, from, 8);
  const svg = canvas(page);
  const start = await mapToScreen(svg, from);
  const end = await mapToScreen(svg, {
    x: (from.x + to.x) / 2,
    y: (from.y + to.y) / 2,
  });
  await page.mouse.move(start.x, start.y);
  await page.mouse.down();
  await page.keyboard.down('Alt');
  await page.mouse.move(end.x, end.y, { steps: 8 });
  await page.mouse.up();
  await page.keyboard.up('Alt');
  await page.getByRole('tab', { name: /Verifica/ }).click();
  await expect(page.getByText('Posti sovrapposti')).toBeVisible({
    timeout: 15_000,
  });
  await page.getByRole('button', { name: 'Salva' }).click();
  await expect(page.getByText('Posti sovrapposti')).toBeVisible();
  const after = await getMap(page, marinaId, mapId);
  expect(after.version).toBe(saved.version);
  await page
    .getByRole('toolbar', { name: "Strumenti dell'editor" })
    .getByRole('button', { name: 'Annulla' })
    .click();
  await expect(page.getByTestId('editor-status')).not.toContainText('Modifiche non salvate');
  // After the undo a valid change saves normally.
  await page.keyboard.press('Escape');
  await page.getByRole('tab', { name: 'Proprietà' }).click();
  await page.getByLabel('Nome (inglese)').fill('Harbour');
  await page.getByLabel('Nome (inglese)').press('Enter');
  await page.getByRole('button', { name: 'Salva' }).click();
  await expect(page.getByText(/Mappa salvata/)).toBeVisible();
  expect((await getMap(page, marinaId, mapId)).version).toBe(saved.version + 1);
});
