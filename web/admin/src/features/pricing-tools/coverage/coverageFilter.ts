import type { PriceCoverage } from '@porto/shared/api';

export type CoverageGap = PriceCoverage['gaps'][number];

export type CoverageFilters = {
  contractType: string;
  period: string;
  /** A category code or a band code (the gap row). */
  category: string;
};

export const NO_FILTERS: CoverageFilters = { contractType: '', period: '', category: '' };

/** The row of a gap: its category, else its band. */
export function gapRow(gap: CoverageGap): string {
  return gap.category ?? gap.band ?? '';
}

/** Gaps matching every non-empty filter. */
export function filterGaps(gaps: readonly CoverageGap[], f: CoverageFilters): CoverageGap[] {
  return gaps.filter(
    (g) =>
      (!f.contractType || g.contract_type === f.contractType) &&
      (!f.period || (g.period ?? '') === f.period) &&
      (!f.category || gapRow(g) === f.category),
  );
}

/** Distinct, sorted values of the gaps for one filter, so that the selects only offer existing choices. */
export function filterOptions(gaps: readonly CoverageGap[]): {
  contractTypes: string[];
  periods: string[];
  categories: string[];
} {
  const uniq = (values: string[]) => [...new Set(values.filter((v) => v !== ''))].sort();
  return {
    contractTypes: uniq(gaps.map((g) => g.contract_type)),
    periods: uniq(gaps.map((g) => g.period ?? '')),
    categories: uniq(gaps.map(gapRow)),
  };
}
