// Search params of the storage pages (m9-plan §12.1): ISO dates only, anything else is dropped.

const ISO = /^\d{4}-\d{2}-\d{2}$/;

function iso(v: unknown): string | undefined {
  return typeof v === 'string' && ISO.test(v) ? v : undefined;
}

export type DateSearch = { date?: string };

export function validateDateSearch(s: Record<string, unknown>): DateSearch {
  const date = iso(s.date);
  return date ? { date } : {};
}

export type CompareSearch = { with?: string; date?: string };

export function validateCompareSearch(s: Record<string, unknown>): CompareSearch {
  const out: CompareSearch = {};
  const date = iso(s.date);
  if (date) out.date = date;
  if (typeof s.with === 'string' && /^[0-9a-zA-Z-]{1,36}$/.test(s.with)) out.with = s.with;
  return out;
}

export type NewContractSearch = { area_id?: string };

export function validateNewContractSearch(s: Record<string, unknown>): NewContractSearch {
  return typeof s.area_id === 'string' && /^[0-9a-fA-F-]{36}$/.test(s.area_id)
    ? { area_id: s.area_id }
    : {};
}
