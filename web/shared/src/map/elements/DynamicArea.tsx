import { memo, useMemo } from 'react';

import type { DynamicArea as AreaEl, Structure } from '../../api/schema';
import { surfaceAreaOf } from '../footprint';
import { boxCentre, boundsOf, svgPoints } from '../geometry';
import { linearAreaPolygon } from '../scene';
import { MAP_TOKENS, elementClass, type ElementState } from '../tokens';
import { ElementShell, type Lod } from './common';

export type DynamicAreaProps = {
  refId: string;
  el: AreaEl;
  structure?: Structure;
  state: ElementState;
  lod: Lod;
  /** Draw the cell grid of surface areas (cells large enough on screen). */
  showCells: boolean;
  interactive: boolean;
  label: string;
};

/** Surface area cells in map coordinates (grid rotation is a multiple of 90°, so cells stay axis-aligned). */
function SurfaceCells({ el }: { el: AreaEl }) {
  const area = useMemo(
    () =>
      surfaceAreaOf({
        outline: el.outline ?? [],
        grid_rotation_dd: el.grid_rotation_dd,
        cell_cm: el.cell_cm ?? 100,
        margin_cm: el.margin_cm ?? 0,
        blocked_cells: el.blocked_cells,
      }),
    [el.outline, el.grid_rotation_dd, el.cell_cm, el.margin_cm, el.blocked_cells],
  );
  const pivot = el.outline?.[0] ?? { x: 0, y: 0 };
  const blocked = new Set(el.blocked_cells);
  const exits = new Set(el.exits);
  const cells = [];
  for (let r = 0; r < area.rows; r++) {
    for (let c = 0; c < area.cols; c++) {
      const idx = r * area.cols + c;
      const cls = blocked.has(idx)
        ? MAP_TOKENS.areaBlocked
        : exits.has(idx)
          ? MAP_TOKENS.areaExit
          : area.usable[idx]
            ? MAP_TOKENS.areaCell
            : null;
      if (!cls) continue;
      cells.push(
        <rect
          key={idx}
          x={area.originX + c * area.cell}
          y={area.originY + r * area.cell}
          width={area.cell}
          height={area.cell}
          className={cls}
          strokeWidth={0.5}
          vectorEffect="non-scaling-stroke"
        />,
      );
    }
  }
  // Grid space is the outline rotated by −grid_rotation around its first vertex: rotate back for display.
  return <g transform={`rotate(${el.grid_rotation_dd / 10} ${pivot.x} ${pivot.y})`}>{cells}</g>;
}

/** Linear area band along a structure side, or surface polygon with its cell grid. */
export const DynamicAreaElement = memo(function DynamicAreaElement({
  refId,
  el,
  structure,
  state,
  lod,
  showCells,
  interactive,
  label,
}: DynamicAreaProps) {
  const pts = el.kind === 'surface' ? (el.outline ?? []) : linearAreaPolygon(el, structure);
  if (pts.length < 3) return null;
  const centre = boxCentre(boundsOf(pts));
  return (
    <ElementShell refId={refId} kind="area" state={state} interactive={interactive} label={label}>
      <polygon
        points={svgPoints(pts)}
        className={elementClass(
          MAP_TOKENS.area,
          el.active === false && state === 'normal' ? 'inactive' : state,
        )}
        strokeWidth={1}
        strokeDasharray="6 3"
        vectorEffect="non-scaling-stroke"
      />
      {el.kind === 'surface' && showCells ? <SurfaceCells el={el} /> : null}
      {lod === 'labelled' ? (
        <text
          x={centre.x}
          y={centre.y}
          fontSize={200}
          textAnchor="middle"
          dominantBaseline="central"
          className={MAP_TOKENS.label}
        >
          {el.code}
        </text>
      ) : null}
    </ElementShell>
  );
});
