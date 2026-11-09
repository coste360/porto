import { describe, expect, it } from 'vitest';

import {
  absSlot,
  addDays,
  buildAxis,
  cellsRange,
  dateHeaders,
  dragReducer,
  dragResult,
  fitLabel,
  IDLE,
  keyReducer,
  paintOrder,
  pointAt,
  pointX,
  rangeRect,
  segmentClass,
  segmentDashed,
  shiftRange,
  slotLabel,
  type Axis,
} from './model';

const bands = [
  { code: 'morning', from_slot: 16, to_slot: 26 },
  { code: 'afternoon', from_slot: 28, to_slot: 38 },
];

function hours(): Axis {
  return buildAxis({
    view: 'hours',
    from: '2027-06-12',
    days: 1,
    openFrom: 16,
    openTo: 38,
    bands,
    cellWidth: 20,
  });
}

describe('planner model', () => {
  it('formats and shifts dates and slots', () => {
    expect(slotLabel(0)).toBe('00:00');
    expect(slotLabel(19)).toBe('09:30');
    expect(slotLabel(48)).toBe('24:00');
    expect(addDays('2027-03-27', 2)).toBe('2027-03-29');
    expect(absSlot('2027-06-12', { date: '2027-06-13', slot: 2 })).toBe(50);
    expect(pointAt('2027-06-12', 48)).toEqual({ date: '2027-06-13', slot: 0 });
    expect(pointAt('2027-06-12', 48, true)).toEqual({ date: '2027-06-12', slot: 48 });
    expect(
      shiftRange(
        { start: { date: '2027-06-12', slot: 46 }, end: { date: '2027-06-12', slot: 48 } },
        2,
      ),
    ).toEqual({
      start: { date: '2027-06-13', slot: 0 },
      end: { date: '2027-06-13', slot: 2 },
    });
  });

  it('builds the three zoom levels', () => {
    const h = hours();
    // Open window 08:00-19:00 +/- 2 hours = 06:00-21:00.
    expect(h.cells).toHaveLength(30);
    expect(h.cells[0]).toMatchObject({ from: 12, to: 13, label: '06:00', dayStart: true });
    expect(h.width).toBe(600);
    const hd = buildAxis({
      view: 'half_days',
      from: '2027-06-12',
      days: 7,
      openFrom: 16,
      openTo: 38,
      bands,
    });
    expect(hd.cells).toHaveLength(14);
    expect(hd.cells[1]).toMatchObject({
      date: '2027-06-12',
      band: 'afternoon',
      from: 28,
      to: 38,
      dayStart: false,
    });
    const d = buildAxis({
      view: 'days',
      from: '2027-06-28',
      days: 14,
      openFrom: 16,
      openTo: 38,
      bands,
    });
    expect(d.cells).toHaveLength(14);
    expect(d.cells[3]).toMatchObject({ date: '2027-07-01', from: 16, to: 38, label: '1' });
  });

  it('maps slots to x and back through cells', () => {
    const h = hours();
    // 10:00 is slot 20: 8 cells after 06:00.
    expect(pointX(h, { date: '2027-06-12', slot: 20 })).toBe(160);
    expect(
      rangeRect(h, {
        start: { date: '2027-06-12', slot: 20 },
        end: { date: '2027-06-12', slot: 26 },
      }),
    ).toEqual({ x: 160, w: 120 });
    // Outside the axis: clamped, and nothing drawn when fully before.
    expect(pointX(h, { date: '2027-06-11', slot: 40 })).toBe(0);
    expect(pointX(h, { date: '2027-06-13', slot: 2 })).toBe(600);
    expect(
      rangeRect(h, {
        start: { date: '2027-06-11', slot: 20 },
        end: { date: '2027-06-11', slot: 26 },
      }),
    ).toBeNull();
    expect(cellsRange(h, 12, 8)).toEqual({
      start: { date: '2027-06-12', slot: 20 },
      end: { date: '2027-06-12', slot: 25 },
    });
    // Days: a multi-day booking spans proportionally and a point between days snaps to the next day.
    const d = buildAxis({
      view: 'days',
      from: '2027-06-12',
      days: 3,
      openFrom: 16,
      openTo: 38,
      bands,
      cellWidth: 22,
    });
    expect(pointX(d, { date: '2027-06-12', slot: 27 })).toBe(11);
    expect(pointX(d, { date: '2027-06-12', slot: 44 })).toBe(22);
    expect(cellsRange(d, 0, 2)).toEqual({
      start: { date: '2027-06-12', slot: 16 },
      end: { date: '2027-06-14', slot: 38 },
    });
    // Half days: a cell is a band.
    const hd = buildAxis({
      view: 'half_days',
      from: '2027-06-12',
      days: 2,
      openFrom: 16,
      openTo: 38,
      bands,
    });
    expect(cellsRange(hd, 2, 2)).toEqual({
      start: { date: '2027-06-13', slot: 16 },
      end: { date: '2027-06-13', slot: 26 },
    });
  });

  it('reduces a drag to create', () => {
    const h = hours();
    let s = dragReducer(IDLE, { type: 'down', rowId: 'u1', cell: 8 });
    s = dragReducer(s, { type: 'over', rowId: 'u1', cell: 13 });
    // Another row does not extend a create.
    s = dragReducer(s, { type: 'over', rowId: 'u2', cell: 20 });
    expect(dragResult(h, s)).toEqual({
      type: 'create',
      rowId: 'u1',
      range: { start: { date: '2027-06-12', slot: 20 }, end: { date: '2027-06-12', slot: 26 } },
    });
    expect(dragReducer(s, { type: 'cancel' })).toBe(IDLE);
    expect(dragResult(h, IDLE)).toBeNull();
  });

  it('reduces a drag to move, with a row change and per-view deltas', () => {
    const h = hours();
    const segment = {
      id: 'b1',
      rowId: 'u1',
      range: { start: { date: '2027-06-12', slot: 20 }, end: { date: '2027-06-12', slot: 24 } },
    };
    let s = dragReducer(IDLE, { type: 'down', rowId: 'u1', cell: 9, segment });
    expect(dragResult(h, s)).toBeNull(); // no move yet
    s = dragReducer(s, { type: 'over', rowId: 'u2', cell: 11 });
    expect(dragResult(h, s)).toEqual({
      type: 'move',
      segmentId: 'b1',
      fromRowId: 'u1',
      rowId: 'u2',
      range: { start: { date: '2027-06-12', slot: 22 }, end: { date: '2027-06-12', slot: 26 } },
    });
    // Days view: one cell to the right is one day later, same times.
    const d = buildAxis({
      view: 'days',
      from: '2027-06-12',
      days: 5,
      openFrom: 16,
      openTo: 38,
      bands,
    });
    let m = dragReducer(IDLE, { type: 'down', rowId: 'u1', cell: 0, segment });
    m = dragReducer(m, { type: 'over', rowId: 'u1', cell: 2 });
    expect(dragResult(d, m)).toMatchObject({
      range: { start: { date: '2027-06-14', slot: 20 }, end: { date: '2027-06-14', slot: 24 } },
    });
  });

  it('moves the keyboard selection', () => {
    let sel = keyReducer(null, 'ArrowRight', false, 3, 10);
    expect(sel).toEqual({ row: 0, anchor: 0, cell: 0 });
    sel = keyReducer(sel, 'ArrowRight', false, 3, 10);
    sel = keyReducer(sel, 'ArrowRight', true, 3, 10);
    sel = keyReducer(sel, 'ArrowRight', true, 3, 10);
    expect(sel).toEqual({ row: 0, anchor: 1, cell: 3 });
    sel = keyReducer(sel, 'ArrowDown', false, 3, 10);
    expect(sel).toEqual({ row: 1, anchor: 3, cell: 3 });
    sel = keyReducer(sel, 'End', false, 3, 10);
    expect(sel).toEqual({ row: 1, anchor: 9, cell: 9 });
    sel = keyReducer(sel, 'ArrowRight', false, 3, 10);
    expect(sel?.cell).toBe(9);
    sel = keyReducer(sel, 'ArrowUp', false, 3, 10);
    sel = keyReducer(sel, 'ArrowUp', false, 3, 10);
    expect(sel?.row).toBe(0);
    expect(keyReducer(sel, 'x', false, 3, 10)).toBe(sel);
    expect(keyReducer(null, 'ArrowRight', false, 0, 10)).toBeNull();
  });

  it('colours segments by state with theme tokens', () => {
    expect(segmentClass('hold', 'held')).toContain('fill-chart-4/40');
    expect(segmentDashed('hold', 'held')).toBe(true);
    expect(segmentClass('booked', 'confirmed')).toContain('fill-chart-1');
    expect(segmentClass('booked', 'checked_out')).toContain('fill-chart-2');
    expect(segmentClass('booked', 'returned')).toContain('fill-chart-3');
    expect(segmentClass('booked', 'closed')).toContain('fill-muted');
    expect(segmentClass('booked', 'no_show')).toContain('fill-destructive/30');
    expect(segmentClass('buffer', 'confirmed')).toContain('fill-muted/60');
    expect(segmentClass('block')).toContain('fill-secondary');
    expect(segmentDashed('booked', 'confirmed')).toBe(false);
  });
});

describe('header labels', () => {
  const axisOf = (view: 'hours' | 'half_days' | 'days', from: string, days: number) =>
    buildAxis({ view, from, days, openFrom: 16, openTo: 38, bands });

  it('writes every date when a date is wide enough (half days)', () => {
    const h = dateHeaders(axisOf('half_days', '2027-06-12', 3), 72);
    expect(h.map((x) => [x.date, x.compact])).toEqual([
      ['2027-06-12', false],
      ['2027-06-13', false],
      ['2027-06-14', false],
    ]);
  });

  it('writes a compact month on the first day and on month starts when days are narrow', () => {
    const h = dateHeaders(axisOf('days', '2027-06-20', 14), 72);
    expect(h.map((x) => [x.date, x.compact])).toEqual([
      ['2027-06-20', true],
      ['2027-07-01', true],
    ]);
    // A month start right after the first cell would overlap it: skipped.
    expect(dateHeaders(axisOf('days', '2027-06-30', 14), 72).map((x) => x.date)).toEqual([
      '2027-06-30',
    ]);
  });

  it('writes no date on the hours view', () => {
    expect(dateHeaders(axisOf('hours', '2027-06-12', 1), 72)).toEqual([]);
  });

  it('cuts a column label to its width', () => {
    expect(fitLabel('Mattina', 56)).toBe('Mattina');
    expect(fitLabel('Pomeriggio', 56)).toBe('Pomerig…');
    expect(fitLabel('Pomeriggio', 8)).toBe('');
  });
});

describe('paintOrder', () => {
  it('draws buffers and blocks before bookings', () => {
    const order = paintOrder([
      { key: 'a', bookingId: 'b1', kind: 'booked' },
      { key: 'b', kind: 'buffer' },
      { key: 'c', bookingId: 'b2', kind: 'hold' },
      { key: 'd', kind: 'block' },
    ]);
    expect(order.map((s) => s.key)).toEqual(['b', 'd', 'a', 'c']);
  });
});
