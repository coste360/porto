import { describe, expect, it } from 'vitest';

import {
  draftItems,
  initialLayout,
  isDirty,
  layoutReducer,
  saveBody,
  unplaced,
  type LayoutItem,
  type LayoutState,
} from './layoutSession';

const item = (id: string, x: number, version = 1): LayoutItem => ({
  allocationId: id,
  version,
  vessel: { id: `v-${id}`, name: id, loa_cm: 1000, beam_cm: 350, hull_type: 'monohull_sail' },
  placement: { x },
});

function loaded(): LayoutState {
  return layoutReducer(initialLayout('area', '2026-11-07'), {
    type: 'load',
    base: [item('a', 0), item('b', 500)],
  });
}

describe('layout session reducer', () => {
  it('moves, removes and adds vessels in the draft', () => {
    let s = loaded();
    expect(isDirty(s)).toBe(false);
    s = layoutReducer(s, { type: 'move', allocationId: 'a', placement: { x: 500 } });
    s = layoutReducer(s, { type: 'move', allocationId: 'b', placement: { x: 0 } });
    expect(draftItems(s).map((d) => [d.allocationId, d.placement?.x])).toEqual([
      ['a', 500],
      ['b', 0],
    ]);
    s = layoutReducer(s, { type: 'add', item: { ...item('c', 1000), placement: null } });
    expect(unplaced(s).map((d) => d.allocationId)).toEqual(['c']);
    s = layoutReducer(s, { type: 'move', allocationId: 'c', placement: { x: 1000 } });
    expect(unplaced(s)).toEqual([]);
    s = layoutReducer(s, { type: 'remove', allocationId: 'a' });
    expect(unplaced(s).map((d) => d.allocationId)).toEqual(['a']);
  });

  it('exchanges two vessels in one save with every version in the base', () => {
    let s = loaded();
    s = layoutReducer(s, { type: 'move', allocationId: 'a', placement: { x: 500 } });
    s = layoutReducer(s, { type: 'move', allocationId: 'b', placement: { x: 0 } });
    expect(saveBody(s)).toEqual({
      date: '2026-11-07',
      base: [
        { allocation_id: 'a', version: 1 },
        { allocation_id: 'b', version: 1 },
      ],
      placements: [
        { allocation_id: 'a', x_cm: 500 },
        { allocation_id: 'b', x_cm: 0 },
      ],
    });
  });

  it('an add of a vessel already in the area becomes a move (drag inside the area from the ops map)', () => {
    const s = layoutReducer(loaded(), {
      type: 'load',
      base: [item('a', 0), item('b', 500)],
      ops: [{ type: 'add', item: item('a', 900) }],
    });
    expect(draftItems(s).find((d) => d.allocationId === 'a')?.placement).toEqual({ x: 900 });
    expect(saveBody(s).placements).toEqual([
      { allocation_id: 'a', x_cm: 900 },
      { allocation_id: 'b', x_cm: 500 },
    ]);
  });

  it('restore discards the draft', () => {
    let s = layoutReducer(loaded(), { type: 'move', allocationId: 'a', placement: { x: 900 } });
    s = layoutReducer(s, { type: 'restore' });
    expect(isDirty(s)).toBe(false);
  });

  it('re-applies the draft on a stale base and names the vessels that changed', () => {
    let s = layoutReducer(loaded(), { type: 'move', allocationId: 'a', placement: { x: 900 } });
    s = layoutReducer(s, { type: 'move', allocationId: 'b', placement: { x: 1500 } });
    // Meanwhile b was moved away by someone else and a got a new version.
    s = layoutReducer(s, { type: 'rebase', base: [item('a', 100, 2), item('d', 2000)] });
    expect(s.changed).toEqual(['a', 'b', 'd']);
    expect(draftItems(s).map((d) => [d.allocationId, d.version, d.placement?.x])).toEqual([
      ['a', 2, 900],
      ['d', 1, 2000],
    ]);
    expect(saveBody(s).base).toEqual([
      { allocation_id: 'a', version: 2 },
      { allocation_id: 'd', version: 1 },
    ]);
  });

  it('applies a compaction proposal as moves of the draft', () => {
    const s = layoutReducer(loaded(), {
      type: 'compaction',
      placements: [
        { allocation_id: 'a', x_cm: 0 },
        { allocation_id: 'b', x_cm: 400 },
      ],
    });
    expect(saveBody(s).placements).toEqual([
      { allocation_id: 'a', x_cm: 0 },
      { allocation_id: 'b', x_cm: 400 },
    ]);
  });
});
