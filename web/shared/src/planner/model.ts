// Pure model of the shared time planner (m11-plan §14.2, D27): a piecewise-linear time axis made of cells (30-minute
// slots, half-day bands or whole days of the local wall clock), point <-> x mapping, the drag reducer (create and
// move), the keyboard selection and the colour classes by state. No React, no DOM: everything here is unit tested.

/** A point of the local wall-clock day: slot s is [s x 30 min, (s+1) x 30 min); an end point may be 48 (24:00). */
export type SlotPoint = { date: string; slot: number };

/** A range [start, end) in slots. */
export type SlotRange = { start: SlotPoint; end: SlotPoint };

export type PlannerView = 'hours' | 'half_days' | 'days';

export type PlannerBand = { code: string; from_slot: number; to_slot: number };

export const SLOTS_PER_DAY = 48;

/** A column of the axis: what a click books (date, from, to) and where it is drawn. */
export type AxisCell = {
  index: number;
  date: string;
  from: number;
  to: number;
  x: number;
  w: number;
  /** Header text: "HH:MM" every hour (hours), band code (half days), day of month (days). */
  label: string;
  /** First cell of a date (a heavier grid line). */
  dayStart: boolean;
  band?: string;
};

export type Axis = { view: PlannerView; first: string; cells: AxisCell[]; width: number };

export type AxisInput = {
  view: PlannerView;
  /** First date shown. */
  from: string;
  /** Dates shown (hours: 1). */
  days: number;
  openFrom: number;
  openTo: number;
  bands: readonly PlannerBand[];
  /** Pixel width of one cell. */
  cellWidth?: number;
};

const DAY_MS = 86_400_000;

function toUTC(date: string): number {
  const [y, m, d] = date.split('-').map(Number);
  return Date.UTC(y!, (m ?? 1) - 1, d ?? 1);
}

/** Adds n days to a YYYY-MM-DD date. */
export function addDays(date: string, n: number): string {
  return new Date(toUTC(date) + n * DAY_MS).toISOString().slice(0, 10);
}

/** Days from a to b (b - a). */
export function daysBetween(a: string, b: string): number {
  return Math.round((toUTC(b) - toUTC(a)) / DAY_MS);
}

/** "HH:MM" of a slot (48 = "24:00"). */
export function slotLabel(slot: number): string {
  const s = Math.max(0, Math.min(SLOTS_PER_DAY, slot));
  const h = Math.floor(s / 2);
  return `${String(h).padStart(2, '0')}:${s % 2 === 0 ? '00' : '30'}`;
}

/** Absolute slot index of a point counted from the start of `base`. */
export function absSlot(base: string, p: SlotPoint): number {
  return daysBetween(base, p.date) * SLOTS_PER_DAY + p.slot;
}

/** The point at absolute slot index `abs` from `base`; as an end point, a date boundary is (date, 48). */
export function pointAt(base: string, abs: number, asEnd = false): SlotPoint {
  let day = Math.floor(abs / SLOTS_PER_DAY);
  let slot = abs - day * SLOTS_PER_DAY;
  if (asEnd && slot === 0) {
    day -= 1;
    slot = SLOTS_PER_DAY;
  }
  return { date: addDays(base, day), slot };
}

/** Shifts a range by delta slots, keeping its length. */
export function shiftRange(r: SlotRange, delta: number): SlotRange {
  const base = r.start.date;
  return {
    start: pointAt(base, absSlot(base, r.start) + delta),
    end: pointAt(base, absSlot(base, r.end) + delta, true),
  };
}

/** Builds the axis of a view: hours = one date, 30-minute columns over the open window +/- 2 hours; half days =
 * the bands of each date; days = whole dates. */
export function buildAxis(input: AxisInput): Axis {
  const cells: AxisCell[] = [];
  const view = input.view;
  const width = input.cellWidth ?? (view === 'hours' ? 28 : view === 'half_days' ? 56 : 40);
  const push = (c: Omit<AxisCell, 'index' | 'x' | 'w'>) => {
    cells.push({ ...c, index: cells.length, x: cells.length * width, w: width });
  };
  if (view === 'hours') {
    const lo = Math.max(0, input.openFrom - 4);
    const hi = Math.min(SLOTS_PER_DAY, input.openTo + 4);
    for (let s = lo; s < hi; s++) {
      push({
        date: input.from,
        from: s,
        to: s + 1,
        label: s % 2 === 0 ? slotLabel(s) : '',
        dayStart: s === lo,
      });
    }
  } else {
    const bands = [...input.bands].sort((a, b) => a.from_slot - b.from_slot);
    for (let i = 0; i < Math.max(1, input.days); i++) {
      const date = addDays(input.from, i);
      if (view === 'half_days' && bands.length > 0) {
        bands.forEach((b, j) => {
          push({
            date,
            from: b.from_slot,
            to: b.to_slot,
            label: b.code,
            dayStart: j === 0,
            band: b.code,
          });
        });
      } else {
        push({
          date,
          from: input.openFrom,
          to: input.openTo,
          label: String(Number(date.slice(8, 10))),
          dayStart: true,
        });
      }
    }
  }
  return { view, first: input.from, cells, width: cells.length * width };
}

/** A date written on the first header line. */
export type DateHeader = { index: number; x: number; date: string; compact: boolean };

/** The dates written on the first header line: every date start when one date is at least `minWidth` pixels wide
 * (half days), else a compact month label on the first cell and on the first day of each month (days), skipping a
 * label that would overlap the previous one. The hours view shows one date, written by the date field. */
export function dateHeaders(axis: Axis, minWidth: number): DateHeader[] {
  if (axis.view === 'hours') return [];
  const starts = axis.cells.filter((c) => c.dayStart);
  const span = starts.length > 1 ? starts[1]!.x - starts[0]!.x : axis.width;
  const compact = span < minWidth;
  const out: DateHeader[] = [];
  let last = -Infinity;
  for (const c of starts) {
    if (compact && c.index !== 0 && !c.date.endsWith('-01')) continue;
    if (c.x - last < minWidth) continue;
    out.push({ index: c.index, x: c.x, date: c.date, compact });
    last = c.x;
  }
  return out;
}

/** Average glyph width of the header font (text-xs) used to keep a label inside its column. */
const GLYPH_WIDTH = 6.5;

/** A header label cut with an ellipsis to fit `width` pixels (the column), padding included. */
export function fitLabel(label: string, width: number): string {
  const max = Math.floor((width - 4) / GLYPH_WIDTH);
  if (label.length <= max) return label;
  if (max <= 1) return '';
  return `${label.slice(0, max - 1)}…`;
}

/** The x of a point on the axis: linear inside a cell; a point between cells snaps to the next cell start. */
export function pointX(axis: Axis, p: SlotPoint): number {
  const abs = absSlot(axis.first, p);
  for (const c of axis.cells) {
    const a0 = absSlot(axis.first, { date: c.date, slot: c.from });
    const a1 = absSlot(axis.first, { date: c.date, slot: c.to });
    if (abs < a0) return c.x;
    if (abs <= a1) return c.x + ((abs - a0) / (a1 - a0)) * c.w;
  }
  return axis.width;
}

/** The drawn extent of a range, or null when it falls outside the axis. */
export function rangeRect(axis: Axis, r: SlotRange): { x: number; w: number } | null {
  const x0 = pointX(axis, r.start);
  const x1 = pointX(axis, r.end);
  if (x1 - x0 <= 0) return null;
  return { x: x0, w: x1 - x0 };
}

/** The range booked by the cells i..j (in any order). */
export function cellsRange(axis: Axis, i: number, j: number): SlotRange {
  const a = axis.cells[Math.min(i, j)]!;
  const b = axis.cells[Math.max(i, j)]!;
  return { start: { date: a.date, slot: a.from }, end: { date: b.date, slot: b.to } };
}

// ---- drag (pointer) ----

/** A segment that can be moved (a booking). */
export type MovableSegment = { id: string; rowId: string; range: SlotRange };

export type DragState =
  | { kind: 'idle' }
  | { kind: 'create'; rowId: string; anchor: number; current: number }
  | { kind: 'move'; segment: MovableSegment; rowId: string; anchor: number; current: number };

export type DragAction =
  | { type: 'down'; rowId: string; cell: number; segment?: MovableSegment }
  | { type: 'over'; rowId: string; cell: number }
  | { type: 'cancel' };

export const IDLE: DragState = { kind: 'idle' };

/** The drag reducer: down on a free cell starts a create, down on a segment starts a move; over extends a create
 * on its row or moves the ghost (any row: a row change is a unit change). */
export function dragReducer(state: DragState, action: DragAction): DragState {
  switch (action.type) {
    case 'down':
      if (action.segment) {
        return {
          kind: 'move',
          segment: action.segment,
          rowId: action.rowId,
          anchor: action.cell,
          current: action.cell,
        };
      }
      return { kind: 'create', rowId: action.rowId, anchor: action.cell, current: action.cell };
    case 'over':
      if (state.kind === 'create') {
        return action.rowId === state.rowId ? { ...state, current: action.cell } : state;
      }
      if (state.kind === 'move') {
        return { ...state, rowId: action.rowId, current: action.cell };
      }
      return state;
    case 'cancel':
      return IDLE;
  }
}

/** The slot shift of a move between two cells: slots on the hours view, whole days otherwise. */
export function moveDelta(axis: Axis, from: number, to: number): number {
  const a = axis.cells[from]!;
  const b = axis.cells[to]!;
  if (axis.view === 'hours') {
    return (
      absSlot(axis.first, { date: b.date, slot: b.from }) -
      absSlot(axis.first, { date: a.date, slot: a.from })
    );
  }
  return daysBetween(a.date, b.date) * SLOTS_PER_DAY;
}

export type DragResult =
  | { type: 'create'; rowId: string; range: SlotRange }
  | { type: 'move'; segmentId: string; fromRowId: string; rowId: string; range: SlotRange };

/** What releasing the pointer does, or null (a move that changes nothing). */
export function dragResult(axis: Axis, state: DragState): DragResult | null {
  if (state.kind === 'create') {
    return {
      type: 'create',
      rowId: state.rowId,
      range: cellsRange(axis, state.anchor, state.current),
    };
  }
  if (state.kind === 'move') {
    const delta = moveDelta(axis, state.anchor, state.current);
    if (delta === 0 && state.rowId === state.segment.rowId) return null;
    return {
      type: 'move',
      segmentId: state.segment.id,
      fromRowId: state.segment.rowId,
      rowId: state.rowId,
      range: shiftRange(state.segment.range, delta),
    };
  }
  return null;
}

/** The ghost drawn while dragging. */
export function dragGhost(
  axis: Axis,
  state: DragState,
): { rowId: string; range: SlotRange } | null {
  const r = dragResult(axis, state);
  if (r) return { rowId: r.rowId, range: r.range };
  if (state.kind === 'move') return { rowId: state.rowId, range: state.segment.range };
  return null;
}

// ---- keyboard ----

/** The keyboard selection: a row, an anchor cell and the current cell (Shift extends). */
export type KeySelection = { row: number; anchor: number; cell: number };

/** Moves the keyboard selection: arrows move (Shift+Left/Right extends on the row), Home/End jump; returns the
 * same selection for other keys. */
export function keyReducer(
  sel: KeySelection | null,
  key: string,
  shift: boolean,
  rows: number,
  cells: number,
): KeySelection | null {
  if (rows === 0 || cells === 0) return null;
  const s = sel ?? { row: 0, anchor: 0, cell: 0 };
  if (!sel && key.startsWith('Arrow')) return s;
  const clamp = (v: number, n: number) => Math.max(0, Math.min(n - 1, v));
  switch (key) {
    case 'ArrowLeft':
    case 'ArrowRight': {
      const cell = clamp(s.cell + (key === 'ArrowLeft' ? -1 : 1), cells);
      return { row: s.row, anchor: shift ? s.anchor : cell, cell };
    }
    case 'ArrowUp':
    case 'ArrowDown': {
      const row = clamp(s.row + (key === 'ArrowUp' ? -1 : 1), rows);
      return { row, anchor: s.cell, cell: s.cell };
    }
    case 'Home':
      return { row: s.row, anchor: shift ? s.anchor : 0, cell: 0 };
    case 'End':
      return { row: s.row, anchor: shift ? s.anchor : cells - 1, cell: cells - 1 };
    default:
      return sel;
  }
}

// ---- drawing order ----

/** Segments in drawing order: buffers and blocks first, bookings on top, so a margin never covers a booking. */
export function paintOrder<T extends { bookingId?: string; kind: string }>(
  segments: readonly T[],
): T[] {
  const rank = (s: T) => (s.bookingId && s.kind !== 'buffer' ? 1 : 0);
  return [...segments].sort((a, b) => rank(a) - rank(b));
}

// ---- colours (theme tokens only) ----

export type SegmentKind = 'hold' | 'booked' | 'buffer' | 'block';

/** SVG fill and stroke classes of a segment by slot kind and booking status (m11-plan §14.2). */
export function segmentClass(kind: SegmentKind | string, status?: string): string {
  if (kind === 'buffer') return 'fill-muted/60 stroke-border';
  if (kind === 'block') return 'fill-secondary stroke-border';
  switch (status) {
    case 'held':
      return 'fill-chart-4/40 stroke-chart-4';
    case 'confirmed':
      return 'fill-chart-1 stroke-chart-1';
    case 'checked_out':
      return 'fill-chart-2 stroke-chart-2';
    case 'returned':
      return 'fill-chart-3 stroke-chart-3';
    case 'closed':
      return 'fill-muted stroke-border';
    case 'no_show':
      return 'fill-destructive/30 stroke-destructive';
    default:
      return kind === 'hold' ? 'fill-chart-4/40 stroke-chart-4' : 'fill-chart-1 stroke-chart-1';
  }
}

/** Text class readable on the segment fill in both themes. */
export function segmentTextClass(kind: SegmentKind | string, status?: string): string {
  if (kind === 'booked' && (status === 'confirmed' || status === 'checked_out'))
    return 'fill-background';
  return 'fill-foreground';
}

/** Whether a segment is drawn dashed (held options). */
export function segmentDashed(kind: SegmentKind | string, status?: string): boolean {
  return kind === 'hold' || status === 'held';
}
