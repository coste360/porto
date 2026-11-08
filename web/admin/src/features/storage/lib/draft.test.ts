import { describe, expect, it } from 'vitest';

import {
  EMPTY_DRAFT,
  boxCells,
  changedIds,
  draftReducer,
  isDirty,
  overlappingIds,
  toLayoutInputs,
  trayIds,
  type DraftState,
} from './draft';

const grid = { cellCm: 100, cols: 20, rows: 10 };
const load = (): DraftState =>
  draftReducer(EMPTY_DRAFT, {
    type: 'load',
    grid,
    items: [
      { allocationId: 'a', loa: 800, beam: 300, at: { x: 0, y: 0, rotation: 0 }, pinned: false },
      {
        allocationId: 'b',
        loa: 650,
        beam: 250,
        at: { x: 1000, y: 500, rotation: 0 },
        pinned: true,
      },
      { allocationId: 't', loa: 500, beam: 200, at: null, pinned: false },
    ],
  });

describe('storage layout draft', () => {
  it('moves by cells with the keyboard, clamped to the grid', () => {
    let s = draftReducer(load(), { type: 'moveBy', id: 'a', cols: 3, rows: 2 });
    expect(s.items.a!.at).toEqual({ x: 300, y: 200, rotation: 0 });
    s = draftReducer(s, { type: 'moveBy', id: 'a', cols: -10, rows: 100 });
    expect(s.items.a!.at).toEqual({ x: 0, y: 700, rotation: 0 });
  });

  it('snaps a drag to cells and rotates in place', () => {
    let s = draftReducer(load(), { type: 'moveTo', id: 'a', x: 457, y: 199 });
    expect(s.items.a!.at).toEqual({ x: 400, y: 100, rotation: 0 });
    s = draftReducer(s, { type: 'rotate', id: 'a' });
    expect(s.items.a!.at!.rotation).toBe(900);
    expect(boxCells(s.items.a!, 900, 100)).toEqual({ w: 3, h: 8 });
  });

  it('never moves a pinned boat', () => {
    const s = draftReducer(load(), { type: 'moveBy', id: 'b', cols: 1, rows: 0 });
    expect(s.items.b!.at).toEqual({ x: 1000, y: 500, rotation: 0 });
  });

  it('places from the tray, unplaces to it, fills a preset and restores', () => {
    let s = load();
    expect(trayIds(s)).toEqual(['t']);
    s = draftReducer(s, { type: 'moveTo', id: 't', x: 1500, y: 0 });
    s = draftReducer(s, { type: 'unplace', id: 'a' });
    expect(trayIds(s)).toEqual(['a']);
    s = draftReducer(s, {
      type: 'fill',
      placements: [
        { allocationId: 'a', at: { x: 0, y: 800, rotation: 0 } },
        { allocationId: 'b', at: { x: 0, y: 0, rotation: 0 } },
      ],
    });
    expect(s.items.a!.at).toEqual({ x: 0, y: 700, rotation: 0 });
    expect(s.items.b!.at).toEqual({ x: 1000, y: 500, rotation: 0 });
    expect(changedIds(s)).toEqual(['a', 't']);
    s = draftReducer(s, { type: 'restore' });
    expect(isDirty(s)).toBe(false);
  });

  it('treats a half turn as the same rectangle and flags client overlaps', () => {
    let s = draftReducer(load(), { type: 'rotate', id: 'a' });
    s = draftReducer(s, { type: 'rotate', id: 'a' });
    expect(isDirty(s)).toBe(false);
    s = draftReducer(s, { type: 'moveTo', id: 'a', x: 900, y: 400 });
    expect([...overlappingIds(s)].sort()).toEqual(['a', 'b']);
    expect(toLayoutInputs(s)[2]).toEqual({
      allocation_id: 't',
      x_cm: null,
      y_cm: null,
      rotation_dd: null,
      pinned: false,
    });
  });
});
