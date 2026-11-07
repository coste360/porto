import AxeBuilder from '@axe-core/playwright';
import { expect, type Page } from '@playwright/test';

import { marinaIdOf } from './api';

// Registry helpers of the M1 specs: synthetic data only (example.invalid emails, checksum-valid fake codes).

type Json = Record<string, unknown>;

async function csrf(page: Page): Promise<string> {
  const cookies = await page.context().cookies();
  return cookies.find((c) => c.name === 'porto_csrf')?.value ?? '';
}

/** Calls the admin API with the page session (CSRF double submit, Idempotency-Key on POST). */
export async function adminApi<T>(
  page: Page,
  method: 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE',
  path: string,
  body?: unknown,
  headers: Record<string, string> = {},
): Promise<{ data: T; etag: string | null }> {
  const res = await page.request.fetch(path, {
    method,
    ...(body !== undefined ? { data: body } : {}),
    headers: {
      'X-CSRF-Token': await csrf(page),
      ...(method === 'POST' ? { 'Idempotency-Key': crypto.randomUUID() } : {}),
      ...headers,
    },
  });
  expect(res.ok(), `${method} ${path}: ${res.status()} ${await res.text()}`).toBeTruthy();
  const text = await res.text();
  return {
    data: (text ? JSON.parse(text) : undefined) as T,
    etag: res.headers()['etag'] ?? null,
  };
}

export const marinaApi = (page: Page) => `/api/admin/v1/marinas/${marinaIdOf(page)}`;

/** Unique suffix so that specs can run on the same database more than once. */
export const uniq = () => Date.now().toString(36).slice(-5).toUpperCase();

/** A checksum-valid fake Italian personal fiscal code built from a random body. */
export function fakeFiscalCode(): string {
  const odd = [
    1, 0, 5, 7, 9, 13, 15, 17, 19, 21, 2, 4, 18, 20, 11, 3, 6, 8, 12, 14, 16, 10, 22, 25, 24, 23,
  ];
  const letters = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ';
  const pick = (s: string) => s[Math.floor(Math.random() * s.length)]!;
  const digit = () => pick('0123456789');
  const body =
    Array.from({ length: 6 }, () => pick(letters)).join('') +
    digit() +
    digit() +
    pick('ABCDEHLMPRST') +
    String(1 + Math.floor(Math.random() * 28)).padStart(2, '0') +
    'Z' +
    digit() +
    digit() +
    digit();
  let sum = 0;
  for (let i = 0; i < 15; i++) {
    const c = body.charCodeAt(i);
    const idx = c >= 48 && c <= 57 ? c - 48 : c - 65;
    sum += i % 2 === 0 ? odd[idx]! : idx;
  }
  return body + letters[sum % 26];
}

export type CreatedCustomer = {
  id: string;
  version: number;
  display_name: string;
};

export async function createCustomer(
  page: Page,
  input: Json & { kind: 'person' | 'company' },
): Promise<CreatedCustomer> {
  const { data } = await adminApi<{ customer: CreatedCustomer }>(
    page,
    'POST',
    `${marinaApi(page)}/customers`,
    input,
  );
  return data.customer;
}

export async function createVessel(
  page: Page,
  input: Json,
): Promise<{ id: string; version: number }> {
  const { data } = await adminApi<{ id: string; version: number }>(
    page,
    'POST',
    `${marinaApi(page)}/vessels`,
    input,
  );
  return data;
}

export async function createDocument(
  page: Page,
  input: Json,
): Promise<{ id: string; version: number }> {
  const { data } = await adminApi<{ id: string; version: number }>(
    page,
    'POST',
    `${marinaApi(page)}/documents`,
    input,
  );
  return data;
}

/** `YYYY-MM-DD` of today + `days` in the marina timezone (Europe/Rome), as the server computes urgency. */
export function dayFromToday(days: number): string {
  const d = new Date(Date.now() + days * 86_400_000);
  return new Intl.DateTimeFormat('en-CA', {
    timeZone: 'Europe/Rome',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  }).format(d);
}

/** `YYYY-MM-DD` → `dd/MM/yyyy` as the UI shows it. */
export const shownDate = (iso: string) => iso.split('-').reverse().join('/');

/**
 * A water map with one straight pier (code `pier`, 100 m long, 3 m wide, along x) saved through saveMap, for
 * the berth import. Returns the map id and its version.
 */
export async function createMapWithPier(
  page: Page,
  name: string,
  pier: string,
): Promise<{ id: string; version: number }> {
  const api = marinaApi(page);
  const { data: map } = await adminApi<Json & { id: string; version: number }>(
    page,
    'POST',
    `${api}/maps`,
    {
      kind: 'water',
      name: { it: name },
      width_cm: 30_000,
      height_cm: 20_000,
      grid_cm: 50,
    },
  );
  const meta = [
    'kind',
    'name',
    'icon',
    'width_cm',
    'height_cm',
    'grid_cm',
    'sort_order',
    'background',
    'georef',
    'concession_outline',
    'water_polygons',
    'land_polygons',
  ];
  const body: Json = Object.fromEntries(meta.map((k) => [k, map[k]]));
  Object.assign(body, {
    structures: [
      {
        key: 'n:1',
        kind: 'pier',
        code: pier,
        points: [
          { x: 5_000, y: 10_000 },
          { x: 15_000, y: 10_000 },
        ],
        width_cm: 300,
        sides: ['left', 'right'],
      },
    ],
    berths: [],
    dynamic_areas: [],
    land_resources: [],
    pedestals: [],
    decorations: [],
  });
  const { data: saved } = await adminApi<{
    document: { id: string; version: number };
  }>(page, 'PUT', `${api}/maps/${map.id}`, body, {
    'If-Match': `"${map.version}"`,
  });
  return { id: saved.document.id, version: saved.document.version };
}

/** Fails on serious or critical axe findings (WCAG 2.x A/AA rules). */
export async function expectNoSeriousViolations(page: Page, label: string) {
  // Popovers and dialogs fade in and out: contrast measured mid-animation is not what the user reads.
  await page.waitForFunction(() =>
    document.getAnimations().every((a) => a.playState !== 'running'),
  );
  const result = await new AxeBuilder({ page })
    .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
    .analyze();
  const serious = result.violations.filter(
    (v) => v.impact === 'serious' || v.impact === 'critical',
  );
  expect(
    serious.map(
      (v) => `${v.id} (${v.impact}): ${v.nodes.map((n) => n.target.join(' ')).join(', ')}`,
    ),
    `${label}: axe serious/critical violations`,
  ).toEqual([]);
}
