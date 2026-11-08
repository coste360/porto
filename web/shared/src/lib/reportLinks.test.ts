import { describe, expect, it } from 'vitest';

import { drillSearch, reportLink, reportQueryString } from './reportLinks';

describe('reportLinks', () => {
  it('builds the report page of a tile with the dashboard period', () => {
    const drill = {
      report: 'occupancy' as const,
      params: { variant: 'physical', family: 'berth', map_kind: 'water', from: '2020-01-01' },
    };
    expect(reportLink('m1', drill, { from: '2026-01-01', to: '2026-11-08' })).toEqual({
      to: '/m/$marinaId/reports/$report',
      params: { marinaId: 'm1', report: 'occupancy' },
      search: {
        variant: 'physical',
        family: 'berth',
        map_kind: 'water',
        from: '2026-01-01',
        to: '2026-11-08',
      },
    });
  });

  it('drops empty values and sorts the query string', () => {
    const s = drillSearch(
      { report: 'charges', params: { basis: 'issued', origin: '' } },
      { from: '2026-11-01', to: '2026-11-08' },
    );
    expect(reportQueryString(s)).toBe('basis=issued&from=2026-11-01&to=2026-11-08');
  });

  it('keeps the own period of a tile whose drill carries from and to', () => {
    const s = drillSearch(
      {
        report: 'storage',
        params: { exits_blocked_only: 'true', from: '2026-11-08', to: '2026-12-07' },
      },
      { from: '2026-01-01', to: '2026-11-08' },
    );
    expect(s).toEqual({ exits_blocked_only: 'true', from: '2026-11-08', to: '2026-12-07' });
  });
});
