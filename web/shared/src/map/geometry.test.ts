import { describe, expect, it } from 'vitest';

import {
  angleDdOf,
  boundsOf,
  divHalfEven,
  normalizeDd,
  offsetPolyline,
  pointAt,
  pointInPolygon,
  polylineLength,
  projectOntoPolyline,
  rectCorners,
  reflectPoint,
  rotatePoint,
  roundHalfEven,
} from './geometry';

describe('roundHalfEven', () => {
  it.each([
    [0.5, 0],
    [1.5, 2],
    [2.5, 2],
    [-0.5, 0],
    [-1.5, -2],
    [2.4999, 2],
    [2.5001, 3],
    [7, 7],
  ])('%f -> %i', (v, want) => expect(roundHalfEven(v)).toBe(want));

  it('divides integers half-even', () => {
    expect(divHalfEven(5, 2)).toBe(2);
    expect(divHalfEven(7, 2)).toBe(4);
    expect(divHalfEven(-5, 2)).toBe(-2);
    expect(divHalfEven(10, 4)).toBe(2);
  });
});

describe('angles', () => {
  it('normalizes and measures', () => {
    expect(normalizeDd(-900)).toBe(2700);
    expect(normalizeDd(3600)).toBe(0);
    expect(angleDdOf(1, 0)).toBe(0);
    expect(angleDdOf(0, 1)).toBe(900); // y down: +y is 90° clockwise
    expect(angleDdOf(-1, 0)).toBe(1800);
    expect(angleDdOf(0, -1)).toBe(2700);
  });
});

describe('rectCorners', () => {
  it('matches geo.Rect.Corners order (anchor-left, anchor-right, far-right, far-left)', () => {
    const c = rectCorners({
      anchor: { x: 0, y: 0 },
      width_cm: 200,
      length_cm: 1000,
      rotation_dd: 0,
    });
    // Looking along +x with y down, left is -y.
    expect(c.map((p) => [Math.round(p.x), Math.round(p.y)])).toEqual([
      [0, -100],
      [0, 100],
      [1000, 100],
      [1000, -100],
    ]);
  });

  it('rotates clockwise on screen', () => {
    const c = rectCorners({
      anchor: { x: 0, y: 0 },
      width_cm: 200,
      length_cm: 1000,
      rotation_dd: 900,
    });
    expect(Math.round(c[2].y)).toBe(1000);
  });
});

describe('polylines', () => {
  const line = [
    { x: 0, y: 0 },
    { x: 1000, y: 0 },
    { x: 1000, y: 500 },
  ];

  it('measures and walks along', () => {
    expect(polylineLength(line)).toBe(1500);
    expect(pointAt(line, 1200)).toMatchObject({ point: { x: 1000, y: 200 }, dd: 900, segment: 1 });
    expect(pointAt(line, 9999).point).toEqual({ x: 1000, y: 500 });
  });

  it('projects a point', () => {
    const p = projectOntoPolyline({ x: 400, y: 30 }, line);
    expect(p?.point).toEqual({ x: 400, y: 0 });
    expect(p?.s).toBe(400);
    expect(p?.dist).toBe(30);
  });

  it('offsets to the right of travel', () => {
    const off = offsetPolyline(line.slice(0, 2), 100);
    expect(off.map((p) => Math.round(p.y))).toEqual([100, 100]);
  });
});

describe('pointInPolygon (inclusive)', () => {
  const sq = [
    { x: 0, y: 0 },
    { x: 10, y: 0 },
    { x: 10, y: 10 },
    { x: 0, y: 10 },
  ];
  it.each([
    [{ x: 5, y: 5 }, true],
    [{ x: 0, y: 5 }, true],
    [{ x: 10, y: 10 }, true],
    [{ x: 11, y: 5 }, false],
    [{ x: -1, y: -1 }, false],
  ])('%o -> %s', (p, want) => expect(pointInPolygon(p, sq)).toBe(want));
});

describe('transforms', () => {
  it('rotates and reflects', () => {
    const r = rotatePoint({ x: 10, y: 0 }, { x: 0, y: 0 }, 900);
    expect([Math.round(r.x), Math.round(r.y)]).toEqual([0, 10]);
    const m = reflectPoint({ x: 3, y: 5 }, { x: 0, y: 0 }, { x: 10, y: 0 });
    expect([Math.round(m.x), Math.round(m.y)]).toEqual([3, -5]);
    expect(
      boundsOf([
        { x: 3, y: -1 },
        { x: -2, y: 4 },
      ]),
    ).toEqual({ minX: -2, minY: -1, maxX: 3, maxY: 4 });
  });
});
