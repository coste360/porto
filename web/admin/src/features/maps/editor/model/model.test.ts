import { describe, expect, it } from 'vitest';

import { buildScene, type SceneEntry } from '@porto/shared/map';

import { berth, makeMapDocument } from '../../testFixtures';
import { calibrate, parseDegreesToDd, scaleFromTwoPoints } from './calibration';
import { addCommand, apply, batch, invert, removeCommand } from './commands';
import { countsOf, createSceneBuilder, fromApi, toSaveRequest, uniqueCode } from './document';
import {
  canRedo,
  canUndo,
  emptyHistory,
  execute,
  historyShortcut,
  MAX_HISTORY,
  redo,
  undo,
} from './history';
import { newKey } from './keys';
import { previewRenumber, renumberCommand } from './renumber';
import { byGroup, byStructure, marquee, toggle } from './selection';
import { DEFAULT_SNAP, snapDelta, snapPoint } from './snap';
import { editorReducer, initialEditorState, isDirty } from './state';
import {
  alignCommand,
  distributeCommand,
  duplicateCommand,
  mirrorCommand,
  moveCommand,
  rotateCommand,
} from './transform';

describe('document', () => {
  it('round-trips the API document and counts elements', () => {
    const doc = fromApi(makeMapDocument());
    expect(doc.version).toBe(3);
    expect(countsOf(doc)).toMatchObject({ structures: 1, berths: 3 });
    const req = toSaveRequest(doc);
    expect(req.berths.map((b) => b.code)).toEqual(['A-1', 'A-2', 'A-3']);
    expect(req.width_cm).toBe(50_000);
    expect('id' in req).toBe(false);
  });

  it('keeps scene items of unchanged elements (memoised rendering)', () => {
    const build = createSceneBuilder();
    const doc = fromApi(makeMapDocument());
    const a = build(doc);
    const moved = apply(doc, moveCommand(doc, ['b1'], 100, 0));
    const b = build(moved);
    expect(b.find((i) => i.ref === 'b2')).toBe(a.find((i) => i.ref === 'b2'));
    expect(b.find((i) => i.ref === 'b1')).not.toBe(a.find((i) => i.ref === 'b1'));
  });

  it('finds the next free code', () => {
    expect(uniqueCode('A-1', new Set(['A-1', 'A-2']))).toBe('A-3');
    expect(uniqueCode('B-009', new Set(['B-009']))).toBe('B-010');
    expect(uniqueCode('X', new Set(['X']))).toBe('X-2');
  });

  it('generates session-unique keys', () => {
    const a = newKey();
    const b = newKey();
    expect(a).not.toBe(b);
    expect(a).toMatch(/^n:\d+$/);
  });
});

describe('commands and history', () => {
  it('applies and inverts, one undo step per action', () => {
    const doc = fromApi(makeMapDocument());
    let h = emptyHistory;
    const r1 = execute(doc, h, moveCommand(doc, ['b1', 'b2'], 50, -20));
    h = r1.history;
    expect(r1.doc.elements.get('b1')).toMatchObject({ el: { anchor: { x: 1200, y: 980 } } });
    const r2 = undo(r1.doc, h);
    expect(r2.doc.elements.get('b1')).toEqual(doc.elements.get('b1'));
    expect(canUndo(r2.history)).toBe(false);
    expect(canRedo(r2.history)).toBe(true);
    const r3 = redo(r2.doc, r2.history);
    expect(r3.doc.elements.get('b2')).toMatchObject({ el: { anchor: { x: 1200, y: 1380 } } });
  });

  it('caps the history at 200 commands', () => {
    let doc = fromApi(makeMapDocument());
    let h = emptyHistory;
    for (let i = 0; i < MAX_HISTORY + 20; i++) {
      const r = execute(doc, h, moveCommand(doc, ['b1'], 10, 0));
      doc = r.doc;
      h = r.history;
    }
    expect(h.past).toHaveLength(MAX_HISTORY);
  });

  it('removing a structure clears the references to it in the same command', () => {
    const doc = fromApi(makeMapDocument());
    const cmd = removeCommand(doc, new Set(['pier-a']));
    const next = apply(doc, cmd);
    expect(next.elements.has('pier-a')).toBe(false);
    const b1 = next.elements.get('b1');
    expect(b1?.kind === 'berth' ? b1.el.structure_id : 'x').toBeUndefined();
    expect(invert(next, cmd).elements.get('b1')).toEqual(doc.elements.get('b1'));
  });

  it('batches commands into one change per element', () => {
    const doc = fromApi(makeMapDocument());
    const c1 = moveCommand(doc, ['b1'], 10, 0);
    const c2 = moveCommand(apply(doc, c1), ['b1'], 10, 0);
    const b = batch('x', [c1, c2]);
    expect(b.changes).toHaveLength(1);
    expect(apply(doc, b).elements.get('b1')).toMatchObject({ el: { anchor: { x: 1170 } } });
  });

  it('maps keyboard shortcuts', () => {
    expect(historyShortcut({ key: 'z', ctrlKey: true, metaKey: false, shiftKey: false })).toBe(
      'undo',
    );
    expect(historyShortcut({ key: 'Z', ctrlKey: false, metaKey: true, shiftKey: true })).toBe(
      'redo',
    );
    expect(historyShortcut({ key: 'y', ctrlKey: true, metaKey: false, shiftKey: false })).toBe(
      'redo',
    );
    expect(
      historyShortcut({ key: 'z', ctrlKey: false, metaKey: false, shiftKey: false }),
    ).toBeNull();
  });

  it('tracks dirty state through the reducer', () => {
    let s = editorReducer(initialEditorState, { type: 'load', doc: fromApi(makeMapDocument()) });
    expect(isDirty(s)).toBe(false);
    s = editorReducer(s, { type: 'execute', command: moveCommand(s.doc!, ['b1'], 10, 0) });
    expect(isDirty(s)).toBe(true);
    s = editorReducer(s, { type: 'undo' });
    expect(isDirty(s)).toBe(false);
  });
});

describe('selection', () => {
  it('selects by structure, group, marquee and shift toggle', () => {
    const doc = fromApi(makeMapDocument());
    expect([...byStructure(doc, 'pier-a')].sort()).toEqual(['b1', 'b2', 'b3', 'pier-a']);
    expect([...byGroup(doc, 'g1')]).toEqual(['b3']);
    expect([...toggle(new Set(['b1']), 'b1')]).toEqual([]);
    const items = buildScene([...doc.elements.values()]);
    const sel = marquee(items, { minX: 1100, minY: 700, maxX: 2500, maxY: 1700 });
    expect([...sel].sort()).toEqual(['b1', 'b2']);
  });
});

describe('snap', () => {
  const doc = fromApi(makeMapDocument());
  const items = buildScene([...doc.elements.values()]);
  const ctx = { items, gridCm: 50, toleranceCm: 80, options: DEFAULT_SNAP };

  it('anchors on a pier edge, perpendicular and outward', () => {
    // Pier axis runs +y at x = 1000; its left edge (counter-clockwise on screen) is at x = 1150.
    const r = snapPoint(
      { x: 1170, y: 5003 },
      { ...ctx, options: { ...DEFAULT_SNAP, vertices: false, align: false } },
    );
    expect(r.kind).toBe('edge');
    expect(r.point).toEqual({ x: 1150, y: 5003 });
    expect(r.rotation_dd).toBe(0); // +y rotated by −90° = +x
    expect(r.side).toBe('left');
    expect(r.structureRef).toBe('pier-a');
    const right = snapPoint(
      { x: 840, y: 5000 },
      { ...ctx, options: { ...DEFAULT_SNAP, vertices: false, align: false } },
    );
    expect(right.side).toBe('right');
    expect(right.rotation_dd).toBe(1800);
  });

  it('falls back to the grid, and Alt disables snapping', () => {
    expect(snapPoint({ x: 20_013, y: 20_037 }, ctx)).toMatchObject({
      kind: 'grid',
      point: { x: 20_000, y: 20_050 },
    });
    expect(snapPoint({ x: 20_013.4, y: 20_037.6 }, { ...ctx, disabled: true })).toMatchObject({
      kind: 'none',
      point: { x: 20_013, y: 20_038 },
    });
    expect(snapDelta(74, 26, 50, false)).toEqual({ x: 50, y: 50 });
  });

  it('aligns with other berth anchors', () => {
    const r = snapPoint(
      { x: 5000, y: 1030 },
      { ...ctx, options: { ...DEFAULT_SNAP, grid: false, edges: false, vertices: false } },
    );
    expect(r.kind).toBe('align');
    expect(r.point.y).toBe(1000);
  });
});

describe('transform', () => {
  const doc = fromApi(makeMapDocument());
  const items = buildScene([...doc.elements.values()]);

  it('rotates around a pivot with integer results', () => {
    const next = apply(doc, rotateCommand(doc, ['b1'], 900, { x: 1150, y: 1000 }));
    expect(next.elements.get('b1')).toMatchObject({
      el: { anchor: { x: 1150, y: 1000 }, rotation_dd: 900 },
    });
  });

  it('mirrors across the pier axis swapping sides', () => {
    const next = apply(doc, mirrorCommand(doc, ['b1'], { x: 1000, y: 0 }, { x: 1000, y: 10_000 }));
    expect(next.elements.get('b1')).toMatchObject({
      el: { anchor: { x: 850, y: 1000 }, rotation_dd: 1800, side: 'right' },
    });
  });

  it('aligns and distributes', () => {
    const aligned = apply(doc, alignCommand(doc, items, new Set(['b1', 'b3']), 'top'));
    const ys = ['b1', 'b3'].map((r) => {
      const e = aligned.elements.get(r);
      return e?.kind === 'berth' ? e.el.anchor.y : 0;
    });
    expect(ys[0]).toBe(ys[1]);
    const three = fromApi(
      makeMapDocument({
        berths: [
          berth('x1', { anchor: { x: 0, y: 5000 } }),
          berth('x2', { anchor: { x: 3000, y: 5000 } }),
          berth('x3', { anchor: { x: 10_000, y: 5000 } }),
        ],
      }),
    );
    const out = apply(
      three,
      distributeCommand(
        three,
        buildScene([...three.elements.values()]),
        new Set(['x1', 'x2', 'x3']),
        'x',
      ),
    );
    const x2 = out.elements.get('x2');
    expect(x2?.kind === 'berth' && x2.el.anchor.x).toBe(5000);
  });

  it('duplicates with new keys and unique codes', () => {
    const { command, newRefs } = duplicateCommand(doc, items, new Set(['b1']));
    const next = apply(doc, command);
    expect(newRefs).toHaveLength(1);
    const copy = next.elements.get(newRefs[0]!);
    expect(copy?.kind === 'berth' && copy.el.code).toBe('A-4');
    expect(copy?.kind === 'berth' && copy.el.id).toBeUndefined();
    expect(copy?.kind === 'berth' && copy.el.anchor).toEqual({ x: 1150, y: 1400 });
  });
});

describe('renumber', () => {
  it('previews odd left / even right and flags conflicts', () => {
    const doc = fromApi(makeMapDocument());
    const rows = previewRenumber(doc, new Set(['b1', 'b2', 'b3']), {
      scheme: 'odd_left_even_right',
      prefix: 'A-',
      start: 1,
      pad: 0,
    });
    expect(rows.map((r) => [r.ref, r.to])).toEqual([
      ['b1', 'A-1'],
      ['b2', 'A-3'],
      ['b3', 'A-2'],
    ]);
    expect(rows.some((r) => r.conflict)).toBe(false);
    const next = apply(doc, renumberCommand(doc, rows));
    const b3 = next.elements.get('b3');
    expect(b3?.kind === 'berth' && b3.el.code).toBe('A-2');
    expect(b3?.kind === 'berth' && b3.el.id).toBe('b3');
  });

  it('per side with padding', () => {
    const doc = fromApi(makeMapDocument());
    const rows = previewRenumber(doc, new Set(['b1', 'b3']), {
      scheme: 'per_side',
      prefix: 'B-',
      start: 1,
      pad: 2,
    });
    expect(rows.map((r) => r.to)).toEqual(['B-L01', 'B-R01']);
  });
});

describe('calibration', () => {
  it('computes the scale half-even and keeps the first point fixed', () => {
    expect(scaleFromTwoPoints(1000, 10_000)).toBe(10_000);
    expect(scaleFromTwoPoints(3, 1)).toBe(333);
    expect(scaleFromTwoPoints(0, 100)).toBeNull();
    const bg = {
      file_id: 'f',
      content_type: 'image/png' as const,
      width_px: 2000,
      height_px: 1000,
      origin: { x: 0, y: 0 },
      scale_mcm_per_px: 10_000,
      rotation_dd: 0,
      opacity_pct: 60,
      visible: true,
    };
    // Two points 1000 px apart under 10 cm/px are 10 000 cm apart; they are really 100 m = 10 000 cm... 50 m:
    const next = calibrate(bg, { x: 1000, y: 0 }, { x: 11_000, y: 0 }, 5000);
    expect(next?.scale_mcm_per_px).toBe(5000);
    expect(next?.origin).toEqual({ x: 500, y: 0 });
  });

  it('parses degrees', () => {
    expect(parseDegreesToDd('12,5')).toBe(125);
    expect(parseDegreesToDd('-90')).toBe(2700);
    expect(parseDegreesToDd('abc')).toBeNull();
  });
});

describe('addCommand', () => {
  it('adds entries keyed by their client key', () => {
    const doc = fromApi(makeMapDocument());
    const entry: SceneEntry = {
      kind: 'berth',
      el: { ...berth('tmp'), id: undefined, key: 'n:999' },
    };
    const next = apply(doc, addCommand('x', [entry]));
    expect(next.elements.has('n:999')).toBe(true);
  });
});
