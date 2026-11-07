import type { PriceListDocument, PriceMatrixParams } from '@porto/shared/api';

import { CHANNELS, CONTRACT_TYPES, CUSTOMER_CATEGORIES } from '../lib/enums';

// URL search of the matrix and print pages: the same parameters as getPriceMatrix, validated so that a
// hand-edited URL never reaches the API with an unknown enum.

export type MatrixSearch = Omit<PriceMatrixParams, 'contract_type'> & {
  contract_type?: PriceMatrixParams['contract_type'];
};

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const ISO_DATE = /^\d{4}-\d{2}-\d{2}$/;

function pick<T extends string>(value: unknown, allowed: readonly T[]): T | undefined {
  return (allowed as readonly unknown[]).includes(value) ? (value as T) : undefined;
}

export function validateMatrixSearch(search: Record<string, unknown>): MatrixSearch {
  const out: MatrixSearch = {};
  const contractType = pick(search.contract_type, CONTRACT_TYPES);
  if (contractType) out.contract_type = contractType;
  const rows = pick(search.rows, ['bands', 'categories'] as const);
  if (rows) out.rows = rows;
  if (typeof search.item_id === 'string' && UUID.test(search.item_id)) out.item_id = search.item_id;
  const channel = pick(search.channel, CHANNELS);
  if (channel) out.channel = channel;
  const category = pick(search.customer_category, CUSTOMER_CATEGORIES);
  if (category) out.customer_category = category;
  if (typeof search.quoted_on === 'string' && ISO_DATE.test(search.quoted_on))
    out.quoted_on = search.quoted_on;
  return out;
}

/** Rows default to the bands when the list has some, else to the categories in use. */
export function defaultRows(doc: Pick<PriceListDocument, 'bands'>): 'bands' | 'categories' {
  return doc.bands.length > 0 ? 'bands' : 'categories';
}
