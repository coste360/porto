import { useId, useReducer, useState, type KeyboardEvent } from 'react';

import {
  cellsRange,
  dateHeaders,
  dragGhost,
  dragReducer,
  dragResult,
  fitLabel,
  IDLE,
  keyReducer,
  paintOrder,
  rangeRect,
  segmentClass,
  segmentDashed,
  segmentTextClass,
  type Axis,
  type KeySelection,
  type MovableSegment,
  type SlotRange,
} from './model';

/** A row of the planner (a unit or a skipper). */
export type PlannerGridRow = {
  id: string;
  label: string;
  sublabel?: string;
  /** Rows are drawn in order; a new group draws a separator. */
  group?: string;
  /** No create or move target (a skipper row, a retired unit). */
  readOnly?: boolean;
};

/** A run of slots on a row. */
export type PlannerGridSegment = {
  key: string;
  rowId: string;
  /** Set for bookings: a booking segment can be clicked and moved. */
  bookingId?: string;
  blockId?: string;
  kind: string;
  status?: string;
  range: SlotRange;
  /** Short text drawn inside (number, status). */
  label: string;
  /** Accessible name. */
  ariaLabel: string;
};

export type PlannerGridProps = {
  axis: Axis;
  rows: readonly PlannerGridRow[];
  segments: readonly PlannerGridSegment[];
  /** Accessible name of the grid. */
  label: string;
  /** Accessible name of a free cell: row label, cell header text. */
  cellLabel: (row: PlannerGridRow, cellIndex: number) => string;
  /** Header text of a date (half days view, and days view when a day is wide enough). */
  dateLabel?: (date: string) => string;
  /** Compact header text of the month of a date (days view with narrow days); default MM/YYYY. */
  monthLabel?: (date: string) => string;
  onCreate?: (rowId: string, range: SlotRange) => void;
  onMove?: (bookingId: string, fromRowId: string, rowId: string, range: SlotRange) => void;
  onSegment?: (segment: PlannerGridSegment) => void;
  /** Booking or block ids drawn highlighted (a 409 names them). */
  highlight?: readonly string[];
  rowHeight?: number;
  labelWidth?: number;
};

const HEADER = 36;
/** Width a full date label needs on the first header line (a dd/mm/yyyy date at text-xs). */
const DATE_LABEL_WIDTH = 72;

const defaultMonthLabel = (date: string) => `${date.slice(5, 7)}/${date.slice(0, 4)}`;

/**
 * Shared time planner (m11-plan §14.2): an SVG grid of rows by time cells (30-minute slots, half-day bands or days,
 * see buildAxis). Drag on free cells to create (the ghost snaps to cells), drag a booking to move it (a row change
 * is a unit change); click a booking to open it. Keyboard: arrows move the selection, Shift+arrows extend it,
 * Enter creates, Escape clears (the SVG is an application region: rows are groups, free cells carry a title). Segments are coloured by state with theme tokens.
 */
export function PlannerGrid(props: PlannerGridProps) {
  const { axis, rows, segments, rowHeight = 32, labelWidth = 160 } = props;
  const [drag, dispatch] = useReducer(dragReducer, IDLE);
  const [sel, setSel] = useState<KeySelection | null>(null);
  const clipBase = `pc${useId().replace(/[^a-zA-Z0-9_-]/g, '')}`;
  const height = HEADER + rows.length * rowHeight;
  const width = labelWidth + axis.width;
  const rowIndex = new Map(rows.map((r, i) => [r.id, i]));
  const highlight = new Set(props.highlight ?? []);
  const writable = (rowId: string) => {
    const r = rows[rowIndex.get(rowId) ?? -1];
    return !!r && !r.readOnly;
  };

  const finish = () => {
    const res = dragResult(axis, drag);
    dispatch({ type: 'cancel' });
    if (!res && drag.kind === 'move') {
      // A press on a booking without moving opens it.
      const seg = segments.find((x) => x.bookingId === drag.segment.id);
      if (seg) props.onSegment?.(seg);
      return;
    }
    if (!res || !writable(res.rowId)) return;
    if (res.type === 'create') props.onCreate?.(res.rowId, res.range);
    else props.onMove?.(res.segmentId, res.fromRowId, res.rowId, res.range);
  };

  const onKeyDown = (e: KeyboardEvent<SVGSVGElement>) => {
    if (e.key === 'Escape') {
      setSel(null);
      dispatch({ type: 'cancel' });
      return;
    }
    if (e.key === 'Enter' && sel) {
      const row = rows[sel.row];
      if (row && !row.readOnly) props.onCreate?.(row.id, cellsRange(axis, sel.anchor, sel.cell));
      e.preventDefault();
      return;
    }
    const next = keyReducer(sel, e.key, e.shiftKey, rows.length, axis.cells.length);
    if (next !== sel) {
      e.preventDefault();
      setSel(next);
    }
  };

  const ghost = dragGhost(axis, drag);
  const ghostRect = ghost ? rangeRect(axis, ghost.range) : null;
  const selRect = sel ? rangeRect(axis, cellsRange(axis, sel.anchor, sel.cell)) : null;

  return (
    <div className="overflow-x-auto rounded-md border" data-testid="planner-grid">
      <svg
        role="application"
        aria-label={props.label}
        tabIndex={0}
        width={width}
        height={height}
        className="bg-card text-xs outline-none select-none focus-visible:ring-2 focus-visible:ring-ring"
        onKeyDown={onKeyDown}
        onPointerUp={finish}
        onPointerLeave={() => dispatch({ type: 'cancel' })}
      >
        {/* Header: dates on the first line, cell labels on the second. */}
        <g className="fill-muted-foreground">
          {dateHeaders(axis, DATE_LABEL_WIDTH).map((h) => (
            <text key={`d${h.index}`} x={labelWidth + h.x + 4} y={14} data-testid="planner-date">
              {h.compact
                ? (props.monthLabel ?? defaultMonthLabel)(h.date)
                : props.dateLabel
                  ? props.dateLabel(h.date)
                  : h.date}
            </text>
          ))}
          {axis.cells.map((c) => (
            <text key={`h${c.index}`} x={labelWidth + c.x + 2} y={30}>
              {axis.view === 'hours' ? c.label : fitLabel(c.label, c.w)}
              {c.label && axis.view !== 'hours' ? <title>{c.label}</title> : null}
            </text>
          ))}
        </g>
        {rows.map((r, i) => {
          const y = HEADER + i * rowHeight;
          const newGroup = i > 0 && r.group !== rows[i - 1]!.group;
          return (
            <g key={r.id} role="group" aria-label={r.label}>
              {newGroup ? (
                <line x1={0} x2={width} y1={y} y2={y} className="stroke-foreground" />
              ) : null}
              <text x={8} y={y + rowHeight / 2 + 4} className="fill-foreground font-medium">
                {r.label}
                {r.sublabel ? (
                  <tspan className="fill-muted-foreground font-normal">{` ${r.sublabel}`}</tspan>
                ) : null}
              </text>
              {axis.cells.map((c) => (
                <rect
                  key={c.index}
                  data-testid={`planner-cell-${r.label}-${c.index}`}
                  x={labelWidth + c.x}
                  y={y}
                  width={c.w}
                  height={rowHeight}
                  className={`stroke-border fill-card ${r.readOnly ? '' : 'hover:fill-muted cursor-cell'}`}
                  strokeWidth={c.dayStart ? 1 : 0.5}
                  onPointerDown={(e) => {
                    if (r.readOnly || e.button !== 0) return;
                    dispatch({ type: 'down', rowId: r.id, cell: c.index });
                  }}
                  onPointerEnter={() => dispatch({ type: 'over', rowId: r.id, cell: c.index })}
                >
                  <title>{props.cellLabel(r, c.index)}</title>
                </rect>
              ))}
            </g>
          );
        })}
        {paintOrder(segments).map((s) => {
          const i = rowIndex.get(s.rowId);
          const rect = rangeRect(axis, s.range);
          if (i === undefined || !rect) return null;
          const y = HEADER + i * rowHeight + 3;
          const id = s.bookingId ?? s.blockId ?? '';
          const movable: MovableSegment | undefined = s.bookingId
            ? { id: s.bookingId, rowId: s.rowId, range: s.range }
            : undefined;
          const hot = id !== '' && highlight.has(id);
          return (
            <g
              key={s.key}
              role={s.bookingId ? 'button' : 'img'}
              aria-label={s.ariaLabel}
              tabIndex={s.bookingId ? 0 : -1}
              data-testid={s.bookingId ? 'planner-booking' : `planner-${s.kind}`}
              data-booking-id={s.bookingId}
              data-status={s.status}
              className={
                s.bookingId && drag.kind === 'idle'
                  ? 'cursor-grab focus-visible:outline-none'
                  : 'pointer-events-none'
              }
              onPointerDown={(e) => {
                if (!movable || e.button !== 0 || !props.onMove) return;
                e.stopPropagation();
                const box = (
                  e.currentTarget as SVGGElement
                ).ownerSVGElement?.getBoundingClientRect();
                const px = box && box.width > 0 ? e.clientX - box.left - labelWidth : rect.x;
                const cell = axis.cells.findIndex((c) => c.x + c.w > px);
                dispatch({
                  type: 'down',
                  rowId: s.rowId,
                  cell: Math.max(0, cell),
                  segment: movable,
                });
              }}
              onClick={() => {
                // Without moves the press never starts a drag: the click opens the booking.
                if (!props.onMove && s.bookingId) props.onSegment?.(s);
              }}
              onKeyDown={(e) => {
                if ((e.key === 'Enter' || e.key === ' ') && s.bookingId) {
                  e.stopPropagation();
                  e.preventDefault();
                  props.onSegment?.(s);
                }
              }}
            >
              <rect
                x={labelWidth + rect.x + 1}
                y={y}
                width={Math.max(rect.w - 2, 2)}
                height={rowHeight - 6}
                rx={3}
                className={`${segmentClass(s.kind, s.status)} ${hot ? 'stroke-destructive' : ''}`}
                strokeWidth={hot ? 3 : 1}
                strokeDasharray={segmentDashed(s.kind, s.status) ? '4 2' : undefined}
              />
              {rect.w > 24 && s.label ? (
                <>
                  <clipPath id={`${clipBase}-${s.key}`}>
                    <rect
                      x={labelWidth + rect.x + 1}
                      y={y}
                      width={Math.max(rect.w - 4, 2)}
                      height={rowHeight - 6}
                    />
                  </clipPath>
                  <text
                    x={labelWidth + rect.x + 5}
                    y={y + rowHeight / 2 + 1}
                    clipPath={`url(#${clipBase}-${s.key})`}
                    className={`${segmentTextClass(s.kind, s.status)} pointer-events-none`}
                  >
                    {s.label}
                  </text>
                </>
              ) : null}
              <title>{s.ariaLabel}</title>
            </g>
          );
        })}
        {ghost && ghostRect && rowIndex.has(ghost.rowId) ? (
          <rect
            data-testid="planner-ghost"
            x={labelWidth + ghostRect.x}
            y={HEADER + rowIndex.get(ghost.rowId)! * rowHeight + 1}
            width={ghostRect.w}
            height={rowHeight - 2}
            className="fill-primary/20 stroke-primary pointer-events-none"
            strokeDasharray="3 2"
          />
        ) : null}
        {sel && selRect ? (
          <rect
            data-testid="planner-selection"
            x={labelWidth + selRect.x}
            y={HEADER + sel.row * rowHeight + 1}
            width={selRect.w}
            height={rowHeight - 2}
            className="fill-none stroke-ring pointer-events-none"
            strokeWidth={2}
          />
        ) : null}
      </svg>
    </div>
  );
}
