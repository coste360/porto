import { describe, expect, it } from 'vitest';

import { SpatialIndex } from './spatialIndex';

describe('SpatialIndex', () => {
  it('returns only items intersecting the box, in insertion order', () => {
    const items = Array.from({ length: 100 }, (_, i) => ({
      box: { minX: i * 500, minY: 0, maxX: i * 500 + 400, maxY: 400 },
      item: i,
    }));
    const index = new SpatialIndex(items);
    expect(index.query({ minX: 950, minY: 0, maxX: 2100, maxY: 10 })).toEqual([2, 3, 4]);
    expect(index.query({ minX: -100, minY: 500, maxX: 100, maxY: 900 })).toEqual([]);
  });

  it('keeps huge boxes in a shared bucket', () => {
    const index = new SpatialIndex([
      { box: { minX: 0, minY: 0, maxX: 10_000_000, maxY: 10_000_000 }, item: 'water' },
      { box: { minX: 10, minY: 10, maxX: 20, maxY: 20 }, item: 'b' },
    ]);
    expect(
      index.query({ minX: 5_000_000, minY: 5_000_000, maxX: 5_000_100, maxY: 5_000_100 }),
    ).toEqual(['water']);
    expect(index.size).toBe(2);
  });
});
