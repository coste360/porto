import { describe, expect, it } from 'vitest';

import {
  addDays,
  compareRows,
  daysBetween,
  fitBoatName,
  planDraftItems,
  presentOn,
  timelineRows,
  timelineTicks,
} from './plan';

describe('storage plan helpers', () => {
  it('keeps boats only and reads positions', () => {
    const base = {
      cells: [],
      first_night: '2026-01-01',
      last_night: '2026-01-31',
      arrival_on: '2026-01-01',
      exit_on: '2026-02-01',
      version: 1,
    };
    const items = planDraftItems([
      {
        ...base,
        allocation_id: 'a',
        kind: 'contract',
        x_cm: 100,
        y_cm: 200,
        rotation_dd: 900,
        vessel: { id: 'v', name: 'Aria', loa_cm: 800, beam_cm: 300, hull_type: 'monohull_sail' },
      },
      { ...base, allocation_id: 'b', kind: 'block', x_cm: 0, y_cm: 0, rotation_dd: 0 },
    ]);
    expect(items).toEqual([
      {
        allocationId: 'a',
        loa: 800,
        beam: 300,
        at: { x: 100, y: 200, rotation: 900 },
        pinned: false,
      },
    ]);
    expect(presentOn(base, '2026-01-31')).toBe(true);
    expect(presentOn(base, '2026-02-01')).toBe(false);
  });

  it('sorts the timeline by exit and marks blocked exits', () => {
    const rows = timelineRows(
      [
        {
          allocation_id: 'x',
          vessel: { name: 'Zeta' },
          arrival_on: '2026-01-01',
          exit_on: '2026-05-01',
        },
        {
          allocation_id: 'y',
          vessel: { name: 'Alfa' },
          arrival_on: '2026-01-01',
          exit_on: '2026-04-01',
        },
      ],
      new Set(['x']),
    );
    expect(rows.map((r) => [r.label, r.exitBlocked])).toEqual([
      ['Alfa', false],
      ['Zeta', true],
    ]);
  });

  it('classifies the comparison diffs', () => {
    const end = (id: string, x: number, r: 0 | 900 = 0) => ({
      resource_type: 'dynamic_area' as const,
      resource_id: id,
      x_cm: x,
      y_cm: 0,
      rotation_dd: r,
    });
    const rows = compareRows({
      scenario_id: 's',
      with: 'real',
      date: '2026-01-01',
      left: {} as never,
      right: {} as never,
      diffs: [
        { allocation_id: 'c', left: end('A', 0) },
        { allocation_id: 'b', left: end('A', 0), right: end('A', 0, 900) },
        { allocation_id: 'a', left: end('A', 0), right: end('A', 500) },
        { allocation_id: 'd', left: end('B', 0), right: end('A', 0) },
      ],
    });
    expect(rows.map((r) => `${r.allocationId}:${r.change}`)).toEqual([
      'a:moved',
      'b:rotated',
      'd:other_resource',
      'c:only_left',
    ]);
  });

  it('does date arithmetic on ISO dates', () => {
    expect(addDays('2026-02-27', 2)).toBe('2026-03-01');
    expect(daysBetween('2026-01-01', '2026-03-01')).toBe(59);
  });
});

describe('timeline axis and boat names', () => {
  it('ticks every 7 days on short windows and on the first of the month on long ones', () => {
    expect(timelineTicks('2026-11-01', '2026-11-20')).toEqual([
      '2026-11-01',
      '2026-11-08',
      '2026-11-15',
    ]);
    expect(timelineTicks('2026-10-15', '2027-01-20')).toEqual([
      '2026-11-01',
      '2026-12-01',
      '2027-01-01',
    ]);
  });

  it('turns the name of a vertical boat and cuts a long name with an ellipsis', () => {
    expect(fitBoatName('Nausicaa VI', 300, 800, 80)).toEqual({
      text: 'Nausicaa VI',
      vertical: true,
    });
    const cut = fitBoatName('Nausicaa VI', 300, 300, 80);
    expect(cut.vertical).toBe(false);
    expect(cut.text.endsWith('…')).toBe(true);
    expect(cut.text.startsWith('Na')).toBe(true);
  });
});
