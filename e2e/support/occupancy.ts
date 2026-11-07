import type { Page } from '@playwright/test';

import {
  addPierThroughApi,
  adminRequest,
  createMap,
  getMap,
  marinaApi,
  type BerthDoc,
  type MapDoc,
} from './map';

// M3 occupancy helpers for the Playwright specs (m3-plan §9.5; signatures fixed). Everything goes through the
// admin API with the page session: a fresh marina per harbour (no price-list overlap with other specs),
// synthetic vessels and customers only.

type Json = Record<string, unknown>;

const uniq = () => Date.now().toString(36).slice(-5) + Math.floor(Math.random() * 1e4).toString(36);

/** `YYYY-MM-DD` of today + `days` in Europe/Rome (the marina timezone of the helpers). */
export function marinaDay(days = 0): string {
  return new Intl.DateTimeFormat('en-CA', {
    timeZone: 'Europe/Rome',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  }).format(new Date(Date.now() + days * 86_400_000));
}

/** Last night of a stay [arrival, departure). */
function lastNight(departure: string): string {
  const [y, m, d] = departure.split('-').map(Number) as [number, number, number];
  const t = new Date(Date.UTC(y, m - 1, d) - 86_400_000);
  return t.toISOString().slice(0, 10);
}

async function createFreshMarina(page: Page): Promise<string> {
  const me = await adminRequest<{ organizations: { id: string }[] }>(
    page,
    'GET',
    '/api/admin/v1/auth/me',
  );
  const slug = `ops-${uniq()}`;
  const marina = await adminRequest<{ id: string }>(
    page,
    'POST',
    `/api/admin/v1/organizations/${me.data.organizations[0]!.id}/marinas`,
    { body: { name: `Marina ${slug}`, slug, timezone: 'Europe/Rome' } },
  );
  return marina.data.id;
}

/**
 * A fresh marina with one map: a pier of `berths` berths (4 m × 12 m, codes `A-1`…), a quay with a linear
 * dynamic area of `linearAreaM` metres (code `L1`, stern-to, 50 cm cells) and 5 parking places (`P1`…`P5`).
 */
export async function setupOpsHarbour(
  page: Page,
  opts?: { berths?: number; linearAreaM?: number },
): Promise<{
  marinaId: string;
  mapId: string;
  berths: BerthDoc[];
  areaId: string;
}> {
  const berths = opts?.berths ?? 20;
  const areaCm = (opts?.linearAreaM ?? 60) * 100;
  const marinaId = await createFreshMarina(page);
  const width = Math.max(60_000, areaCm + 10_000, Math.ceil(berths / 2) * 400 + 20_000);
  const map = await createMap(page, marinaId, 'Porto', width, 40_000);
  await addPierThroughApi(page, marinaId, map.id, {
    code: 'A',
    origin: { x: 10_000, y: 5_000 },
    berthsPerSide: Math.ceil(berths / 2),
    keyPrefix: 'a',
  });
  const doc = await getMap(page, marinaId, map.id);
  const { id: _id, version, updated_at: _u, ...rest } = doc as MapDoc & { updated_at: string };
  const body: Json = {
    ...rest,
    structures: [
      ...((rest.structures as unknown[]) ?? []),
      {
        key: 'q:1',
        kind: 'quay',
        code: 'Q',
        points: [
          { x: 5_000, y: 30_000 },
          { x: 5_000 + areaCm, y: 30_000 },
        ],
        width_cm: 500,
        sides: ['left'],
      },
    ],
    dynamic_areas: [
      {
        key: 'l:1',
        kind: 'linear',
        code: 'L1',
        structure_id: 'q:1',
        side: 'left',
        from_cm: 0,
        to_cm: areaCm,
        mooring_mode: 'stern_to',
        cell_cm: 50,
        margin_cm: 50,
        max_beam_cm: 600,
        blocked_cells: [],
        exits: [],
        grid_rotation_dd: 0,
        active: true,
        group_ids: [],
      },
    ],
    land_resources: Array.from({ length: 5 }, (_, i) => ({
      key: `p:${i + 1}`,
      kind: 'parking',
      code: `P${i + 1}`,
      anchor: { x: 5_000 + i * 700, y: 36_000 },
      rotation_dd: 0,
      width_cm: 250,
      length_cm: 500,
      attributes: [],
      active: true,
      group_ids: [],
    })),
  };
  const saved = await adminRequest<{
    document: MapDoc & { dynamic_areas: { id: string; code: string }[] };
  }>(page, 'PUT', `${marinaApi(marinaId)}/maps/${map.id}`, {
    body,
    ifMatch: version,
  });
  const area = saved.data.document.dynamic_areas.find((a) => a.code === 'L1');
  return {
    marinaId,
    mapId: map.id,
    berths: saved.data.document.berths.slice(0, berths),
    areaId: area!.id,
  };
}

/**
 * Archives a harbour marina made by setupOpsHarbour: the owner signs in to a single marina again (login lands on
 * the picker otherwise). Call it in `test.afterEach` of every spec that sets up a harbour.
 */
export async function archiveOpsHarbour(page: Page, marinaId: string): Promise<void> {
  await adminRequest(page, 'POST', `/api/admin/v1/marinas/${marinaId}/archive`, {
    expectStatus: 204,
  });
}

/**
 * Active transit and annual price lists through the API, valid from 1 January of this year to 31 December of
 * the next: transit 5,00 €/m per night, annual 100,00 €/m per year (VAT 22 % included).
 */
export async function setupPrices(page: Page, marinaId: string): Promise<void> {
  const api = marinaApi(marinaId);
  const year = Number(marinaDay().slice(0, 4));
  const from = `${year}-01-01`;
  const to = `${year + 1}-12-31`;
  const classes = await adminRequest<{ items: { id: string; code: string }[] }>(
    page,
    'GET',
    `${api}/revenue-classes`,
  );
  const classOf = (code: string) =>
    (classes.data.items.find((c) => c.code === code) ?? classes.data.items[0]!).id;
  const item = async (code: string, unit: string, revenue: string) =>
    (
      await adminRequest<{ id: string }>(page, 'POST', `${api}/items`, {
        body: {
          code,
          name: { it: code },
          item_type: 'berth_fee',
          unit,
          vat_rate_bp: 2200,
          price_includes_vat: true,
          revenue_class_id: classOf(revenue),
        },
      })
    ).data.id;
  const trn = await item('TRN-NTT', 'night', 'transit');
  const ann = await item('ORM-ANN', 'year', 'mooring_annual');
  const head = {
    code: `L-${uniq().toUpperCase()}`,
    name: { it: 'Listino occupazione' },
    valid_from: from,
    valid_to: to,
    contract_types: ['transit', 'annual'],
  };
  const created = await adminRequest<{
    id: string;
    version: number;
    settings: Json;
  }>(page, 'POST', `${api}/price-lists`, { body: head });
  const saved = await adminRequest<{ document: { version: number } }>(
    page,
    'PUT',
    `${api}/price-lists/${created.data.id}`,
    {
      ifMatch: created.data.version,
      body: {
        ...head,
        settings: {
          ...created.data.settings,
          dimension_source: 'vessel',
          length_rounding: { step_cm: 10, mode: 'up' },
          primary_items: { transit: trn, annual: ann },
        },
        periods: [
          {
            key: 'p:all',
            code: 'TUTTO',
            name: { it: "Tutto l'anno" },
            kind: 'season',
            ranges: [{ from, to }],
          },
        ],
        bands: [],
        rules: [
          {
            key: 'r:trn',
            stage: 'base',
            name: 'Transito',
            item_id: trn,
            basis: 'per_meter',
            unit: 'night',
            unit_cents: 500,
            scope: { periods: ['TUTTO'] },
          },
          {
            key: 'r:ann',
            stage: 'base',
            name: 'Annuale',
            item_id: ann,
            basis: 'per_meter',
            unit: 'year',
            unit_cents: 10_000,
          },
        ],
      },
    },
  );
  await adminRequest(page, 'POST', `${api}/price-lists/${created.data.id}/activate`, {
    body: {},
    ifMatch: saved.data.document.version,
  });
}

/** A synthetic vessel of the marina (11,34 m × 3,80 m by default). */
export async function createVesselViaApi(
  page: Page,
  marinaId: string,
  o: { name?: string; loaCm?: number; beamCm?: number } = {},
): Promise<{ id: string }> {
  return (
    await adminRequest<{ id: string }>(page, 'POST', `${marinaApi(marinaId)}/vessels`, {
      body: {
        name: o.name ?? `Barca ${uniq()}`,
        hull_type: 'monohull_sail',
        loa_cm: o.loaCm ?? 1134,
        beam_cm: o.beamCm ?? 380,
      },
    })
  ).data;
}

/** A synthetic customer of the marina (a person named "Cliente <last name>"). */
export async function createCustomerViaApi(
  page: Page,
  marinaId: string,
  lastName = `E2e${uniq()}`,
): Promise<{ id: string; display_name: string }> {
  const { data } = await adminRequest<{
    customer: { id: string; display_name: string };
  }>(page, 'POST', `${marinaApi(marinaId)}/customers`, {
    body: { kind: 'person', first_name: 'Cliente', last_name: lastName },
  });
  return data.customer;
}

type ContractResp = {
  id: string;
  version: number;
  allocations: { id: string }[];
};

/** A transit on a berth (or at x in the linear area), optionally confirmed with the manual driver. */
export async function createTransitViaApi(
  page: Page,
  marinaId: string,
  o: {
    vesselId: string;
    berthId?: string;
    areaX?: number;
    arrival: string;
    departure: string;
    confirm?: boolean;
  },
): Promise<{ id: string; allocationId: string }> {
  const api = marinaApi(marinaId);
  let target: Json;
  if (o.berthId) target = { resource_type: 'berth', resource_id: o.berthId };
  else {
    const doc = (await getMap(page, marinaId, await firstMapId(page, marinaId))) as MapDoc & {
      dynamic_areas: { id: string }[];
    };
    target = {
      resource_type: 'dynamic_area',
      resource_id: doc.dynamic_areas[0]!.id,
      x_cm: o.areaX ?? 100, // the first valid x is half the area margin (50 cm), snapped
    };
  }
  const body: Json = {
    vessel_id: o.vesselId,
    arrival: o.arrival,
    departure: o.departure,
    channel: 'desk',
    placements: [{ ...target, first_night: o.arrival, last_night: lastNight(o.departure) }],
  };
  if (o.confirm) {
    // Confirmation needs a customer (customer_required): a synthetic one per transit.
    body.customer_id = (await createCustomerViaApi(page, marinaId)).id;
    body.confirm = { reference: 'e2e', pay_at_departure: true };
  }
  const { data } = await adminRequest<ContractResp>(page, 'POST', `${api}/transits`, { body });
  return { id: data.id, allocationId: data.allocations[0]!.id };
}

async function firstMapId(page: Page, marinaId: string): Promise<string> {
  const { data } = await adminRequest<{ items: { id: string }[] }>(
    page,
    'GET',
    `${marinaApi(marinaId)}/maps`,
  );
  return data.items[0]!.id;
}

/** A mooring contract on a berth, optionally activated. */
export async function createContractViaApi(
  page: Page,
  marinaId: string,
  o: {
    vesselId: string;
    customerId: string;
    berthId: string;
    term: 'annual' | 'seasonal';
    startsOn: string;
    activate?: boolean;
  },
): Promise<{ id: string; allocationId: string }> {
  const [y, m, d] = o.startsOn.split('-').map(Number) as [number, number, number];
  // Annual: one year later (29 Feb → 28 Feb); seasonal: five months, an explicit end.
  const end =
    o.term === 'annual'
      ? new Date(Date.UTC(y + 1, m - 1, Math.min(d, m === 2 ? 28 : d)))
      : new Date(Date.UTC(y, m - 1 + 5, d));
  const endsOn = end.toISOString().slice(0, 10);
  const { data } = await adminRequest<ContractResp>(
    page,
    'POST',
    `${marinaApi(marinaId)}/contracts`,
    {
      body: {
        term: o.term,
        customer_id: o.customerId,
        vessel_id: o.vesselId,
        starts_on: o.startsOn,
        ends_on: endsOn,
        placements: [
          {
            resource_type: 'berth',
            resource_id: o.berthId,
            first_night: o.startsOn,
            last_night: lastNight(endsOn),
          },
        ],
        activate: o.activate ?? false,
      },
    },
  );
  return { id: data.id, allocationId: data.allocations[0]!.id };
}

/** n vessels with confirmed transits on the map, 10 parallel requests at a time (map-perf). */
export async function seedPerfOccupancy(
  page: Page,
  marinaId: string,
  mapId: string,
  n: number,
): Promise<void> {
  const doc = await getMap(page, marinaId, mapId);
  const berths = doc.berths.slice(0, n);
  if (berths.length < n)
    throw new Error(`seedPerfOccupancy: ${n} vessels but ${berths.length} berths`);
  const customer = await createCustomerViaApi(page, marinaId);
  const arrival = marinaDay(0);
  const departure = marinaDay(2);
  const api = marinaApi(marinaId);
  for (let i = 0; i < berths.length; i += 10) {
    await Promise.all(
      berths.slice(i, i + 10).map(async (b, j) => {
        const vessel = await createVesselViaApi(page, marinaId, {
          name: `Perf ${i + j + 1}`,
          loaCm: Math.min(b.length_cm - 100, 600 + ((i + j) % 8) * 100),
          beamCm: Math.min(b.width_cm - 60, 250),
        });
        await adminRequest(page, 'POST', `${api}/transits`, {
          body: {
            vessel_id: vessel.id,
            customer_id: customer.id,
            arrival,
            departure,
            channel: 'desk',
            placements: [
              {
                resource_type: 'berth',
                resource_id: b.id,
                first_night: arrival,
                last_night: lastNight(departure),
              },
            ],
            confirm: { reference: 'perf', pay_at_departure: true },
          },
        });
      }),
    );
  }
}
