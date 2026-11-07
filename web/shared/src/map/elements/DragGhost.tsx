import { memo } from 'react';

import { formatCm, svgPoints, type Pt } from '../geometry';
import { vesselPath } from '../vessel-shape';

export type GhostState = 'ok' | 'warn' | 'conflict' | 'invalid';

const GHOST_CLASS: Record<GhostState, string> = {
  ok: 'fill-harbor-ok/40 stroke-harbor-ok',
  warn: 'fill-harbor-warn/40 stroke-harbor-warn',
  conflict: 'fill-harbor-bad/40 stroke-harbor-bad',
  invalid: 'fill-harbor-bad/20 stroke-harbor-bad',
};

export type DragGhostProps = {
  state: GhostState;
  vessel: { loa: number; beam: number; hull: string };
  /** Stern and heading of the silhouette. */
  origin: Pt;
  rotationDd: number;
  /** Footprint cells (area) or the target berth outline, each a polygon in map cm. */
  cells?: readonly (readonly Pt[])[];
  /** Cells in conflict (indexes into cells). */
  conflictCells?: ReadonlySet<number>;
  /** Free space on each side along a linear axis (cm) and where to write it. */
  free?: { left: number | null; right: number | null; at: Pt; fontCm: number };
  locale?: string;
};

/** Footprint ghost of a dragged vessel with its cell strip coloured ok / warn / conflict (m3-plan §8.5). */
export const DragGhost = memo(function DragGhost({
  state,
  vessel,
  origin,
  rotationDd,
  cells,
  conflictCells,
  free,
  locale = 'it',
}: DragGhostProps) {
  const d = vesselPath({ loa_cm: vessel.loa, beam_cm: vessel.beam, hull_type: vessel.hull });
  return (
    <g className="pointer-events-none" data-testid="ops-drag-ghost" data-state={state}>
      {cells?.map((poly, i) => (
        <polygon
          key={i}
          points={svgPoints(poly)}
          className={conflictCells?.has(i) ? GHOST_CLASS.conflict : GHOST_CLASS[state]}
          strokeWidth={1}
          vectorEffect="non-scaling-stroke"
        />
      ))}
      {d ? (
        <path
          d={d}
          transform={`translate(${origin.x} ${origin.y}) rotate(${rotationDd / 10})`}
          className={GHOST_CLASS[state]}
          strokeWidth={2}
          strokeDasharray="6 4"
          vectorEffect="non-scaling-stroke"
          fillRule="evenodd"
        />
      ) : null}
      {free ? (
        <text
          x={free.at.x}
          y={free.at.y}
          fontSize={free.fontCm}
          textAnchor="middle"
          className="fill-foreground stroke-background font-semibold"
          paintOrder="stroke"
          strokeWidth={3}
          vectorEffect="non-scaling-stroke"
          data-testid="ops-drag-free"
        >
          {`← ${free.left === null ? '—' : formatCm(free.left, locale)}  ·  ${free.right === null ? '—' : formatCm(free.right, locale)} →`}
        </text>
      ) : null}
    </g>
  );
});
