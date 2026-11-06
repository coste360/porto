import { expect, type Locator, type Page } from '@playwright/test';

/** Admin API path of a marina. */
export const marinaApi = (marinaId: string) => `/api/admin/v1/marinas/${marinaId}`;

async function csrf(page: Page): Promise<string> {
  const cookies = await page.context().cookies();
  return cookies.find((c) => c.name === 'porto_csrf')?.value ?? '';
}

/** Calls the admin API with the page session (CSRF header, Idempotency-Key on POST, optional If-Match). */
export async function adminRequest<T>(
  page: Page,
  method: 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE',
  path: string,
  opts: { body?: unknown; ifMatch?: number; expectStatus?: number } = {},
): Promise<{ data: T; status: number; etag: string | null }> {
  const headers: Record<string, string> = { 'X-CSRF-Token': await csrf(page) };
  if (method === 'POST') headers['Idempotency-Key'] = crypto.randomUUID();
  if (opts.ifMatch !== undefined) headers['If-Match'] = `"${opts.ifMatch}"`;
  const res = await page.request.fetch(path, {
    method,
    headers,
    ...(opts.body !== undefined ? { data: opts.body } : {}),
  });
  if (opts.expectStatus !== undefined)
    expect(res.status(), `${method} ${path}`).toBe(opts.expectStatus);
  else expect(res.ok(), `${method} ${path}: ${res.status()} ${await res.text()}`).toBeTruthy();
  const text = await res.text();
  return {
    data: (text ? JSON.parse(text) : undefined) as T,
    status: res.status(),
    etag: res.headers()['etag'] ?? null,
  };
}

export type Pt = { x: number; y: number };
export type BerthDoc = {
  id: string;
  code: string;
  anchor: Pt;
  rotation_dd: number;
  width_cm: number;
  length_cm: number;
};
export type MapDoc = {
  id: string;
  version: number;
  structures: { id: string; kind: string; code: string }[];
  berths: BerthDoc[];
  [k: string]: unknown;
};

export async function getMap(page: Page, marinaId: string, mapId: string): Promise<MapDoc> {
  return (await adminRequest<MapDoc>(page, 'GET', `${marinaApi(marinaId)}/maps/${mapId}`)).data;
}

export async function createMap(
  page: Page,
  marinaId: string,
  name: string,
  widthCm = 60_000,
  heightCm = 40_000,
) {
  return (
    await adminRequest<MapDoc>(page, 'POST', `${marinaApi(marinaId)}/maps`, {
      body: {
        kind: 'water',
        name: { it: name },
        width_cm: widthCm,
        height_cm: heightCm,
        grid_cm: 50,
      },
    })
  ).data;
}

/** Runs the pier generator through the API and saves its elements into the map. */
export async function addPierThroughApi(
  page: Page,
  marinaId: string,
  mapId: string,
  pier: { code: string; origin: Pt; berthsPerSide: number; keyPrefix: string },
): Promise<MapDoc> {
  const gen = await adminRequest<{ elements: Record<string, unknown[]> }>(
    page,
    'POST',
    `${marinaApi(marinaId)}/maps/${mapId}/generate`,
    {
      body: {
        generator: {
          type: 'pier',
          key_prefix: pier.keyPrefix,
          origin: pier.origin,
          rotation_dd: 2700,
          length_cm: pier.berthsPerSide * 400 + 200,
          width_cm: 300,
          code: pier.code,
          sides: ['left', 'right'],
          berths_per_side: pier.berthsPerSide,
          berth_width_cm: 400,
          berth_length_cm: 1200,
          numbering: {
            scheme: 'odd_left_even_right',
            prefix: `${pier.code}-`,
            start: 1,
            pad: 0,
          },
        },
      },
    },
  );
  const current = await getMap(page, marinaId, mapId);
  const { id: _id, version, updated_at: _u, ...rest } = current as MapDoc & { updated_at: string };
  const body: Record<string, unknown> = { ...rest };
  for (const [k, list] of Object.entries(gen.data.elements))
    body[k] = [...((rest[k] as unknown[]) ?? []), ...list];
  const saved = await adminRequest<{ document: MapDoc }>(
    page,
    'PUT',
    `${marinaApi(marinaId)}/maps/${mapId}`,
    {
      body,
      ifMatch: version,
    },
  );
  return saved.data.document;
}

/** The editor or viewer canvas. */
export function canvas(page: Page): Locator {
  return page.locator('svg[data-px-per-cm]').first();
}

/** Screen point of a map point (cm) on a HarborMap svg, from its viewBox and on-screen box. */
export async function mapToScreen(svg: Locator, p: Pt): Promise<Pt> {
  const box = await svg.boundingBox();
  const vb = (await svg.getAttribute('viewBox'))?.split(' ').map(Number) ?? [0, 0, 1, 1];
  if (!box) throw new Error('map not visible');
  const [vx = 0, vy = 0, vw = 1] = vb;
  const s = box.width / vw;
  return { x: box.x + (p.x - vx) * s, y: box.y + (p.y - vy) * s };
}

/** Counts user actions for the acceptance annotations. */
export class ActionCounter {
  count = 0;
  tick(n = 1) {
    this.count += n;
  }
}

export async function clickMap(page: Page, p: Pt, actions?: ActionCounter) {
  const s = await mapToScreen(canvas(page), p);
  await page.mouse.click(s.x, s.y);
  actions?.tick();
}

/** Zooms with the wheel keeping the map point under the cursor. */
export async function zoomAround(page: Page, p: Pt, steps = 5) {
  const svg = canvas(page);
  for (let i = 0; i < steps; i++) {
    const s = await mapToScreen(svg, p);
    await page.mouse.move(s.x, s.y);
    await page.mouse.wheel(0, -400);
  }
}

/** Centre of a berth rectangle (anchor + half the length along its rotation). */
export function berthCentre(b: BerthDoc): Pt {
  const rad = (b.rotation_dd / 10) * (Math.PI / 180);
  return {
    x: b.anchor.x + (Math.cos(rad) * b.length_cm) / 2,
    y: b.anchor.y + (Math.sin(rad) * b.length_cm) / 2,
  };
}

/** Marina id of the current shell URL. */
export function marinaOf(page: Page): string {
  const m = new URL(page.url()).pathname.match(/\/admin\/m\/([^/]+)/);
  if (!m) throw new Error(`not in a marina shell: ${page.url()}`);
  return m[1]!;
}
