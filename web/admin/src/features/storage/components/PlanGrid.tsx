import { useRef, useState, type KeyboardEvent, type PointerEvent } from 'react';

import { accessibilityOf, TOKEN_CLASSES, type AccessibilityStyle } from '@porto/shared/map';
import { cn } from '@porto/shared/lib/utils';

import {
  boxCells,
  boxIndexes,
  type DraftAction,
  type DraftGrid,
  type DraftItem,
} from '../lib/draft';
import { fitBoatName } from '../lib/plan';

// Plan of a surface area at a date (spec 17.2, m9-plan §12.1): the cell grid at scale in an SVG whose units are
// centimetres (ADR 0004), exits and fixed obstacles as cells, one rectangle per boat present that night. Boats
// are dragged with the pointer (snapped to cells) or moved with the keyboard: arrows move one cell, R rotates,
// Delete sends the boat to the "Da collocare" tray. An invalid position is outlined red with its reason.

export type PlanBoat = {
  item: DraftItem;
  label: string;
  /** Kind token of the fill (contract term, transit, owner). */
  token: keyof typeof TOKEN_CLASSES;
  /** Footprint cells from the server while the boat is where the server put it. */
  serverCells?: readonly number[];
  issue?: string;
};

export type PlanGridProps = {
  grid: DraftGrid;
  boats: readonly PlanBoat[];
  exits: readonly number[];
  obstacles: readonly number[];
  selected?: string;
  /** Accessibilità lens styles by allocation id (lens off when undefined). */
  lens?: ReadonlyMap<string, AccessibilityStyle>;
  readOnly?: boolean;
  dispatch?: (a: DraftAction) => void;
  onSelect?: (id: string | undefined) => void;
  ariaLabel: string;
  boatLabel: (b: PlanBoat) => string;
};

type Drag = { id: string; dx: number; dy: number; x: number; y: number };

function cellRect(i: number, g: DraftGrid) {
  return { x: (i % g.cols) * g.cellCm, y: Math.floor(i / g.cols) * g.cellCm, size: g.cellCm };
}

export function PlanGrid({
  grid,
  boats,
  exits,
  obstacles,
  selected,
  lens,
  readOnly,
  dispatch,
  onSelect,
  ariaLabel,
  boatLabel,
}: PlanGridProps) {
  const svg = useRef<SVGSVGElement>(null);
  const [drag, setDrag] = useState<Drag | null>(null);
  const width = grid.cols * grid.cellCm;
  const height = grid.rows * grid.cellCm;

  const toGrid = (e: PointerEvent): { x: number; y: number } | null => {
    const el = svg.current;
    const m = el?.getScreenCTM?.();
    if (!el || !m) return null;
    const p = new DOMPoint(e.clientX, e.clientY).matrixTransform(m.inverse());
    return { x: p.x, y: p.y };
  };

  const onDown = (e: PointerEvent, b: PlanBoat) => {
    onSelect?.(b.item.allocationId);
    if (readOnly || b.item.pinned || !b.item.at) return;
    const p = toGrid(e);
    if (!p) return;
    (e.target as Element).setPointerCapture?.(e.pointerId);
    setDrag({
      id: b.item.allocationId,
      dx: p.x - b.item.at.x,
      dy: p.y - b.item.at.y,
      x: b.item.at.x,
      y: b.item.at.y,
    });
  };
  const onMove = (e: PointerEvent) => {
    if (!drag) return;
    const p = toGrid(e);
    if (p) setDrag({ ...drag, x: p.x - drag.dx, y: p.y - drag.dy });
  };
  const onUp = () => {
    if (!drag) return;
    dispatch?.({
      type: 'moveTo',
      id: drag.id,
      x: Math.round(drag.x + grid.cellCm / 2),
      y: Math.round(drag.y + grid.cellCm / 2),
    });
    setDrag(null);
  };
  const onKey = (e: KeyboardEvent, b: PlanBoat) => {
    const id = b.item.allocationId;
    const step: Record<string, [number, number]> = {
      ArrowLeft: [-1, 0],
      ArrowRight: [1, 0],
      ArrowUp: [0, -1],
      ArrowDown: [0, 1],
    };
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      onSelect?.(id);
      return;
    }
    if (readOnly) return;
    const s = step[e.key];
    if (s) {
      e.preventDefault();
      dispatch?.({ type: 'moveBy', id, cols: s[0], rows: s[1] });
    } else if (e.key === 'r' || e.key === 'R') {
      e.preventDefault();
      dispatch?.({ type: 'rotate', id });
    } else if (e.key === 'Delete' || e.key === 'Backspace') {
      e.preventDefault();
      dispatch?.({ type: 'unplace', id });
    }
  };

  const ghost = drag ? boats.find((b) => b.item.allocationId === drag.id) : undefined;
  const ghostBox = ghost?.item.at
    ? boxCells(ghost.item, ghost.item.at.rotation, grid.cellCm)
    : undefined;

  return (
    <svg
      ref={svg}
      viewBox={`0 0 ${width} ${height}`}
      className="bg-muted h-auto w-full touch-none rounded-md border select-none"
      role="group"
      aria-label={ariaLabel}
      data-testid="storage-plan"
      onPointerMove={onMove}
      onPointerUp={onUp}
      onPointerCancel={() => setDrag(null)}
    >
      <rect x={0} y={0} width={width} height={height} className="fill-background" />
      {exits.map((c) => {
        const r = cellRect(c, grid);
        return (
          <rect
            key={`e${c}`}
            x={r.x}
            y={r.y}
            width={r.size}
            height={r.size}
            className={TOKEN_CLASSES.in.fill}
            data-exit
          />
        );
      })}
      {obstacles.map((c) => {
        const r = cellRect(c, grid);
        return (
          <rect
            key={`o${c}`}
            x={r.x}
            y={r.y}
            width={r.size}
            height={r.size}
            className={TOKEN_CLASSES.blocked.fill}
          />
        );
      })}
      {boats.map((b) => {
        const it = b.item;
        if (!it.at) return null;
        const { w, h } = boxCells(it, it.at.rotation, grid.cellCm);
        // Lens on: every boat takes its accessibility colour (a boat without an event of the day is free).
        const style = lens ? accessibilityOf(lens, it.allocationId) : undefined;
        const token = style ? style.colorToken : b.token;
        const isSel = selected === it.allocationId;
        const cells = b.serverCells ?? boxIndexes(it, grid);
        return (
          <g
            key={it.allocationId}
            role="button"
            tabIndex={0}
            aria-label={boatLabel(b)}
            aria-pressed={isSel}
            data-allocation={it.allocationId}
            data-lens={style?.key}
            data-invalid={b.issue ? 'true' : undefined}
            className="cursor-grab focus-visible:outline-none"
            onPointerDown={(e) => onDown(e, b)}
            onKeyDown={(e) => onKey(e, b)}
          >
            <title>{boatLabel(b)}</title>
            {cells.map((c) => {
              const r = cellRect(c, grid);
              return (
                <rect
                  key={c}
                  x={r.x}
                  y={r.y}
                  width={r.size}
                  height={r.size}
                  className={cn(TOKEN_CLASSES[token].fill, 'opacity-40')}
                />
              );
            })}
            <rect
              x={it.at.x}
              y={it.at.y}
              width={w * grid.cellCm}
              height={h * grid.cellCm}
              rx={grid.cellCm / 4}
              className={cn(
                TOKEN_CLASSES[token].fill,
                b.issue || style?.outline ? TOKEN_CLASSES.bad.stroke : 'stroke-foreground',
                isSel ? 'stroke-primary' : '',
              )}
              strokeWidth={isSel || b.issue || style?.outline ? grid.cellCm / 5 : grid.cellCm / 20}
              strokeDasharray={b.issue ? `${grid.cellCm / 3}` : undefined}
            />
            <BoatName
              x={it.at.x}
              y={it.at.y}
              w={w * grid.cellCm}
              h={h * grid.cellCm}
              font={grid.cellCm * 0.8}
              text={`${style?.badge ? `${style.badge} · ` : ''}${b.label}`}
              className={TOKEN_CLASSES[token].onFill}
            />
          </g>
        );
      })}
      {drag && ghost && ghostBox ? (
        <rect
          x={drag.x}
          y={drag.y}
          width={ghostBox.w * grid.cellCm}
          height={ghostBox.h * grid.cellCm}
          className="fill-primary/20 stroke-primary pointer-events-none"
          strokeWidth={grid.cellCm / 10}
          strokeDasharray={`${grid.cellCm / 3}`}
          data-testid="storage-drag-ghost"
        />
      ) : null}
    </svg>
  );
}

function BoatName({
  x,
  y,
  w,
  h,
  font,
  text,
  className,
}: {
  x: number;
  y: number;
  w: number;
  h: number;
  font: number;
  text: string;
  className: string;
}) {
  const across = Math.min(w, h);
  const size = Math.min(font, across * 0.7);
  const fit = fitBoatName(text, w, h, size);
  const cx = x + w / 2;
  const cy = y + h / 2;
  return (
    <text
      x={cx}
      y={cy}
      textAnchor="middle"
      dominantBaseline="central"
      fontSize={size}
      transform={fit.vertical ? `rotate(-90 ${cx} ${cy})` : undefined}
      className={cn(className, 'pointer-events-none')}
    >
      {fit.text}
    </text>
  );
}
