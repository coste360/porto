import type { Page } from '@playwright/test';

import { adminRequest, marinaApi } from './map';
import { marinaDay } from './occupancy';

// M10 yard helpers for the Playwright specs (QUOTE): the §4.4 antifouling catalogue built through the admin API
// in a fresh marina (items, a yard price list valid this year and next, qualifications, hull_area, layers and the
// ANTIVEG package), a 12 m sailing boat with a draft and a customer with an email.

export type YardWorld = {
  pkg: { id: string; version: number };
  items: Record<string, string>;
  vessel: string;
  vesselName: string;
  customer: string;
  customerName: string;
};

export async function setupYardCatalogue(page: Page, marinaId: string): Promise<YardWorld> {
  const api = marinaApi(marinaId);
  const classes = await adminRequest<{ items: { id: string; code: string }[] }>(page, 'GET', `${api}/revenue-classes`);
  const classOf = (code: string) => (classes.data.items.find((c) => c.code === code) ?? classes.data.items[0]!).id;
  const items: Record<string, string> = {};
  const rules: Record<string, unknown>[] = [];
  for (const [code, type, unit, cls, cents] of [
    ['SRV-ALAG', 'service', 'm', 'services', 1800],
    ['SRV-LAV', 'service', 'm2', 'services', 400],
    ['LAB-HAND', 'labour', 'hour', 'yard_labour', 3800],
    ['LAB-PAINT', 'labour', 'hour', 'yard_labour', 4500],
    ['MAT-AVEG', 'material', 'piece', 'yard_materials', 9500],
    ['SRV-VARO', 'service', 'm', 'services', 1800],
  ] as const) {
    const it = await adminRequest<{ id: string }>(page, 'POST', `${api}/items`, {
      body: {
        code,
        name: { it: code },
        item_type: type,
        unit,
        vat_rate_bp: 2200,
        price_includes_vat: false,
        revenue_class_id: classOf(cls),
      },
    });
    items[code] = it.data.id;
    rules.push({ key: code, stage: 'base', name: code, item_id: it.data.id, basis: 'flat', unit: 'stay', amount_cents: cents });
  }
  const year = Number(marinaDay().slice(0, 4));
  const range = { valid_from: `${year}-01-01`, valid_to: `${year + 1}-12-31` };
  const list = await adminRequest<{ id: string; version: number }>(page, 'POST', `${api}/price-lists`, {
    body: { code: 'CANT', name: { it: 'Cantiere' }, ...range, contract_types: ['storage'] },
  });
  const saved = await adminRequest<{ document: { version: number } }>(page, 'PUT', `${api}/price-lists/${list.data.id}`, {
    ifMatch: list.data.version,
    body: {
      code: 'CANT',
      name: { it: 'Cantiere' },
      ...range,
      contract_types: ['storage'],
      periods: [],
      bands: [],
      rules,
      settings: {
        dimension_source: 'vessel',
        length_rounding: { step_cm: 10, mode: 'up' },
        primary_items: { storage: items['SRV-ALAG'] },
      },
    },
  });
  await adminRequest(page, 'POST', `${api}/price-lists/${list.data.id}/activate`, {
    ifMatch: saved.data.document.version,
    body: {},
  });
  const wt = await adminRequest<{ id: string }>(page, 'POST', `${api}/yard/work-types`, {
    body: { code: 'ANTIFOULING', name: { it: 'Antivegetativa' } },
  });
  for (const [code, name, item, cost] of [
    ['hand', 'Operaio', 'LAB-HAND', 2200],
    ['painter', 'Verniciatore', 'LAB-PAINT', 2600],
  ] as const) {
    await adminRequest(page, 'POST', `${api}/yard/qualifications`, {
      body: { code, name: { it: name }, labour_item_id: items[item], default_cost_cents: cost },
    });
  }
  await adminRequest(page, 'PUT', `${api}/yard/items/${items['MAT-AVEG']}`, { body: { cost_cents: 5800 } });
  await adminRequest(page, 'POST', `${api}/yard/formula-variables`, {
    body: { name: 'hull_area', source: 'derived', kind: 'number', unit: 'm2', expression: 'loa * (beam + draft) * 0.85' },
  });
  await adminRequest(page, 'POST', `${api}/yard/formula-variables`, {
    body: { name: 'layers', source: 'job', kind: 'number', unit: 'pcs', min_milli: 1000, max_milli: 4000, default_milli: 2000 },
  });
  const pkg = await adminRequest<{ id: string; version: number }>(page, 'POST', `${api}/yard/work-packages`, {
    body: {
      code: 'ANTIVEG',
      name: { it: 'Antivegetativa completa' },
      work_type_id: wt.data.id,
      job_vars: ['layers'],
      lines: [
        { seq: 1, item_id: items['SRV-ALAG'], qty_formula: 'loa', price_formula: 'if(hull_type == "catamaran", list_price * 1.5, list_price)' },
        { seq: 2, item_id: items['SRV-LAV'], qty_formula: 'round(hull_area, 1)' },
        { seq: 3, item_id: items['LAB-HAND'], qty_formula: 'round(hull_area * 0.15, 1)', task: { title: { it: 'Carteggiatura' } } },
        { seq: 4, item_id: items['LAB-PAINT'], qty_formula: 'round(hull_area * layers * 0.05, 1)', task: { title: { it: 'Antivegetativa' } } },
        { seq: 5, item_id: items['MAT-AVEG'], qty_formula: 'ceil(hull_area * layers / 25)' },
        { seq: 6, item_id: items['SRV-VARO'], qty_formula: 'loa' },
      ],
    },
  });
  const suffix = Date.now().toString(36).slice(-4);
  const vesselName = `Maestrale ${suffix}`;
  const vessel = await adminRequest<{ id: string }>(page, 'POST', `${api}/vessels`, {
    body: { name: vesselName, loa_cm: 1200, beam_cm: 400, draft_cm: 150, hull_type: 'monohull_sail' },
  });
  const customerName = `Bianchi${suffix}`;
  const customer = await adminRequest<{ customer: { id: string } }>(page, 'POST', `${api}/customers`, {
    body: {
      kind: 'person',
      first_name: 'Anna',
      last_name: customerName,
      emails: [{ label: 'home', value: `anna.${suffix}@example.test` }],
    },
  });
  return {
    pkg: { id: pkg.data.id, version: pkg.data.version },
    items,
    vessel: vessel.data.id,
    vesselName,
    customer: customer.data.customer.id,
    customerName,
  };
}

/** Creates a draft of the ANTIVEG package for the world's boat through the API. */
export async function createAntifoulingQuote(
  page: Page,
  marinaId: string,
  w: YardWorld,
  extra: Record<string, unknown> = {},
): Promise<{ id: string; version: number; number: string }> {
  const r = await adminRequest<{ id: string; version: number; number: string }>(page, 'POST', `${marinaApi(marinaId)}/yard/quotes`, {
    body: { customer_id: w.customer, vessel_id: w.vessel, selections: [{ package_id: w.pkg.id }], ...extra },
  });
  return r.data;
}
