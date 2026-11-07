import type { Schemas } from '@porto/shared/api';

import { isIsoDate } from './dates';

// Search params that carry a desk prefill across pages (search panel → contract wizard / quote editor), parsed
// loosely: unknown or malformed values are dropped.

type ResourceType = Schemas['ResourceType'];
type Term = Schemas['ContractTerm'];

export type PrefillSearch = {
  vessel?: string;
  customer?: string;
  term?: Term;
  from?: string;
  to?: string;
  rt?: ResourceType;
  rid?: string;
  x?: number;
};

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const TERMS: readonly string[] = ['annual', 'seasonal', 'monthly', 'period'];
const TYPES: readonly string[] = ['berth', 'dynamic_area', 'land_resource'];

export function validatePrefillSearch(search: Record<string, unknown>): PrefillSearch {
  const out: PrefillSearch = {};
  const id = (v: unknown) => (typeof v === 'string' && UUID.test(v) ? v : undefined);
  const vessel = id(search.vessel);
  const customer = id(search.customer);
  const rid = id(search.rid);
  if (vessel) out.vessel = vessel;
  if (customer) out.customer = customer;
  if (rid) out.rid = rid;
  if (typeof search.term === 'string' && TERMS.includes(search.term))
    out.term = search.term as Term;
  if (typeof search.rt === 'string' && TYPES.includes(search.rt))
    out.rt = search.rt as ResourceType;
  if (typeof search.from === 'string' && isIsoDate(search.from)) out.from = search.from;
  if (typeof search.to === 'string' && isIsoDate(search.to)) out.to = search.to;
  const x =
    typeof search.x === 'number' ? search.x : typeof search.x === 'string' ? Number(search.x) : NaN;
  if (Number.isSafeInteger(x) && x >= 0) out.x = x;
  return out;
}

/** Contract wizard prefill from a search solution (first segment's resource and the stay start). */
export function contractSearchFromSolution(
  s: Schemas['OccupancySolution'],
  o: { term: Term; vesselId?: string },
): PrefillSearch {
  const seg = s.segments[0];
  const out: PrefillSearch = { term: o.term };
  if (o.vesselId) out.vessel = o.vesselId;
  if (seg) {
    out.rt = seg.resource_type;
    out.rid = seg.resource_id;
    out.from = seg.first_night;
    if (seg.x_cm !== undefined) out.x = seg.x_cm;
  }
  return out;
}

/** The placement carried by a prefill search, or null. */
export function prefillTarget(p: PrefillSearch): Schemas['PlacementInput'] | null {
  if (!p.rt || !p.rid) return null;
  return { resource_type: p.rt, resource_id: p.rid, ...(p.x !== undefined ? { x_cm: p.x } : {}) };
}
