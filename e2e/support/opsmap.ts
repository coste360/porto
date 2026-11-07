import { expect, type Locator, type Page } from '@playwright/test';

import { berthCentre, mapToScreen, type BerthDoc, type Pt } from './map';
import { dayFromToday } from './registry';

// Helpers of the operational map specs (WEB-OPSMAP, m3-plan §9.5). The fixed test ids of m3-plan §8.7 are the
// contract with WEB-DESK's specs.

export const today = () => dayFromToday(0);

/** Opens the operational view of a map on a night and waits for the day view to be drawn. */
export async function openOps(
  page: Page,
  marinaId: string,
  mapId: string,
  opts: { date?: string; lens?: string; mode?: 'scale' | 'schema' } = {},
): Promise<void> {
  const q = new URLSearchParams({ map: mapId, date: opts.date ?? today() });
  if (opts.lens) q.set('lens', opts.lens);
  if (opts.mode) q.set('mode', opts.mode);
  await page.goto(`/admin/m/${marinaId}/ops?${q.toString()}`);
  await expect(opsMap(page)).toHaveAttribute('data-date', opts.date ?? today(), { timeout: 15_000 });
}

export function opsMap(page: Page): Locator {
  return page.getByTestId('ops-map');
}

export function opsSvg(page: Page): Locator {
  return opsMap(page).locator('svg[data-px-per-cm]');
}

/** The lens fill of an element (data-lens carries its legend key). */
export function lensFill(page: Page, ref: string): Locator {
  return opsMap(page).locator(`[data-ref="${ref}"] [data-lens]`).first();
}

export async function legendCount(page: Page, key: string): Promise<number> {
  return Number(await page.getByTestId(`ops-legend-count-${key}`).innerText());
}

/** Screen centre of a berth on the ops map. */
export async function berthOnScreen(page: Page, b: BerthDoc): Promise<Pt> {
  return mapToScreen(opsSvg(page), berthCentre(b));
}

/** Presses at `from`, drags in steps to `to` and optionally keeps the button down (to inspect the ghost). */
export async function dragOnMap(
  page: Page,
  from: Pt,
  to: Pt,
  opts: { release?: boolean; shift?: boolean; steps?: number } = {},
): Promise<void> {
  await page.mouse.move(from.x, from.y);
  await page.mouse.down();
  await page.mouse.move(from.x + 10, from.y + 10, { steps: 2 });
  await page.mouse.move(to.x, to.y, { steps: opts.steps ?? 12 });
  if (opts.release === false) return;
  if (opts.shift) await page.keyboard.down('Shift');
  await page.mouse.up();
  if (opts.shift) await page.keyboard.up('Shift');
}

/** Records requestAnimationFrame intervals while `run` executes; returns avg and p95 (ms). */
export async function measureFrames(
  page: Page,
  run: () => Promise<void>,
): Promise<{ frames: number; avg: number; p95: number }> {
  await page.evaluate(() => {
    const w = window as unknown as { __frames: number[]; __rec: boolean };
    w.__frames = [];
    w.__rec = true;
    let last = performance.now();
    const tick = (t: number) => {
      w.__frames.push(t - last);
      last = t;
      if (w.__rec) requestAnimationFrame(tick);
    };
    requestAnimationFrame(tick);
  });
  await run();
  const frames = await page.evaluate(() => {
    const w = window as unknown as { __frames: number[]; __rec: boolean };
    w.__rec = false;
    return w.__frames.slice(1);
  });
  const sorted = [...frames].sort((a, b) => a - b);
  const p95 = sorted[Math.floor(sorted.length * 0.95)] ?? 0;
  const avg = frames.reduce((s, f) => s + f, 0) / Math.max(1, frames.length);
  return { frames: frames.length, avg, p95 };
}

/** Pans the map back and forth for `ms` with the mouse (background drag). */
export async function panFor(page: Page, ms: number, start: Pt, dx = 200): Promise<void> {
  const until = Date.now() + ms;
  await page.mouse.move(start.x, start.y);
  await page.mouse.down();
  let dir = 1;
  while (Date.now() < until) {
    await page.mouse.move(start.x + dir * dx, start.y, { steps: 20 });
    dir = -dir;
  }
  await page.mouse.up();
}
