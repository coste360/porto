import type { ContractType, PriceListSummary } from '@porto/shared/api';

// Validity timeline of the price list page: one row per contract type and list, one cell per month.

export const GRID_COLS: Record<number, string> = {
  1: 'grid-cols-12',
  2: 'grid-cols-24',
  3: 'grid-cols-36',
  4: 'grid-cols-48',
  5: 'grid-cols-60',
};

/** First and last year shown: the span of the non-archived lists, at most five years. */
export function timelineYears(lists: PriceListSummary[]): { start: number; years: number } | null {
  const live = lists.filter((l) => l.status !== 'archived');
  if (live.length === 0) return null;
  const start = Math.min(...live.map((l) => Number(l.valid_from.slice(0, 4))));
  const end = Math.max(...live.map((l) => Number(l.valid_to.slice(0, 4))));
  return { start, years: Math.min(5, Math.max(1, end - start + 1)) };
}

const pad = (n: number) => String(n).padStart(2, '0');

/** For each month of the span, whether [valid_from, valid_to] touches it. */
export function monthCells(
  list: Pick<PriceListSummary, 'valid_from' | 'valid_to'>,
  start: number,
  years: number,
): { month: string; covered: boolean }[] {
  const out: { month: string; covered: boolean }[] = [];
  for (let y = start; y < start + years; y++) {
    for (let m = 1; m <= 12; m++) {
      const month = `${y}-${pad(m)}`;
      out.push({
        month,
        covered: list.valid_from.slice(0, 7) <= month && month <= list.valid_to.slice(0, 7),
      });
    }
  }
  return out;
}

/** Lists serving each contract type (active and drafts), in validity order. */
export function rowsByContractType(
  lists: PriceListSummary[],
  types: readonly ContractType[],
): { contractType: ContractType; lists: PriceListSummary[] }[] {
  return types
    .map((contractType) => ({
      contractType,
      lists: lists
        .filter((l) => l.status !== 'archived' && l.contract_types.includes(contractType))
        .sort((a, b) => (a.valid_from < b.valid_from ? -1 : a.valid_from > b.valid_from ? 1 : 0)),
    }))
    .filter((r) => r.lists.length > 0);
}
