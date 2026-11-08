import { describe, expect, it } from 'vitest';

import { resources } from '@porto/shared/i18n';
import { drillSearch, reportQueryString } from '@porto/shared/lib/reportLinks';

import { FILTER_VALUES, REPORTS, sectionOf } from './catalog';
import {
  pageOf,
  patchSearch,
  reportQuery,
  urlSearchValues,
  validateReportSearch,
} from './reportSearch';

const ytd = { from: '2026-01-01', to: '2026-11-08' };

describe('reportSearch', () => {
  it('writes numbers and booleans unquoted in the URL and reads them back as strings', () => {
    const search = {
      page: '2',
      new: 'true',
      code: '007',
      from: '2026-01-01',
      big: '99999999999999999999',
    };
    const url = urlSearchValues(search);
    expect(url).toEqual({
      page: 2,
      new: true,
      code: '007',
      from: '2026-01-01',
      big: '99999999999999999999',
    });
    expect(validateReportSearch(url)).toEqual(search);
  });

  it('keeps string values and drops empty ones', () => {
    expect(
      validateReportSearch({ from: '2026-01-01', page: 2, overdue: true, empty: '', obj: {} }),
    ).toEqual({ from: '2026-01-01', page: '2', overdue: 'true' });
  });

  it('round-trips a drill link: the page request equals the tile drill params plus the period', () => {
    const drill = {
      report: 'occupancy' as const,
      params: { variant: 'physical', family: 'berth', map_kind: 'water', from: 'x' },
    };
    const period = { from: '2026-01-01', to: '2026-03-31' };
    const search = drillSearch(drill, period);
    // The router validates the search of the link: nothing is lost or added.
    const validated = validateReportSearch(search);
    expect(validated).toEqual(search);
    const q = reportQuery(validated, ytd);
    expect(q).toEqual({ variant: 'physical', family: 'berth', map_kind: 'water', ...period });
    expect(reportQueryString(q)).toBe(
      'family=berth&from=2026-01-01&map_kind=water&to=2026-03-31&variant=physical',
    );
  });

  it('defaults the period to year to date', () => {
    expect(reportQuery({ basis: 'competence' }, ytd)).toEqual({ basis: 'competence', ...ytd });
  });

  it('resets the page when a filter changes and drops page 1', () => {
    const s = { from: '2026-01-01', page: '3', basis: 'issued' };
    expect(patchSearch(s, { basis: 'competence' })).toEqual({
      from: '2026-01-01',
      basis: 'competence',
    });
    expect(patchSearch(s, { page: '4' })).toEqual({ ...s, page: '4' });
    expect(patchSearch(s, { page: '1' })).toEqual({ from: '2026-01-01', basis: 'issued' });
    expect(patchSearch(s, { basis: undefined })).toEqual({ from: '2026-01-01' });
    expect(pageOf({ page: '3' })).toBe(3);
    expect(pageOf({ page: 'x' })).toBe(1);
    expect(pageOf({})).toBe(1);
  });
});

describe('catalog', () => {
  const it18 = resources.it.reports as unknown as Record<string, Record<string, unknown>>;
  const en18 = resources.en.reports as unknown as Record<string, Record<string, unknown>>;

  it('serves every report key once', () => {
    const keys = REPORTS.map((r) => r.key);
    expect(new Set(keys).size).toBe(16 + 3 + 1); // M7 + M9 (storage, maintenance, utilities) + M10 yard
  });

  it('has an Italian and English label for every column, total, grouping, section and filter', () => {
    for (const r of REPORTS) {
      const sections = r.sections ?? [''];
      const keys = [
        ...r.totals.map((t) => t.key),
        ...sections.flatMap((s) => r.columns(s).map((c) => c.key)),
      ];
      for (const k of keys) {
        expect(it18.columns?.[k], `${r.key} ${k} it`).toBeTruthy();
        expect(en18.columns?.[k], `${r.key} ${k} en`).toBeTruthy();
      }
      for (const g of r.groupBy) {
        expect(it18.groupBy?.[g], `${r.key} group_by ${g}`).toBeTruthy();
        expect(en18.groupBy?.[g], `${r.key} group_by ${g}`).toBeTruthy();
      }
      for (const s of r.sections ?? []) {
        expect((it18.sections?.[r.key] as Record<string, string>)?.[s]).toBeTruthy();
        expect((en18.sections?.[r.key] as Record<string, string>)?.[s]).toBeTruthy();
      }
      for (const f of r.filters) expect(it18.filters?.[f], `${r.key} filter ${f}`).toBeTruthy();
      expect(it18.descriptions?.[r.key]).toBeTruthy();
      expect(en18.descriptions?.[r.key]).toBeTruthy();
    }
    expect(Object.keys(FILTER_VALUES).length).toBeGreaterThan(0);
  });

  it('picks the first section by default', () => {
    const rec = REPORTS.find((r) => r.key === 'receivables')!;
    expect(sectionOf(rec, undefined)).toBe('open');
    expect(sectionOf(rec, 'reminders')).toBe('reminders');
    expect(sectionOf(rec, 'nope')).toBe('open');
  });
});
