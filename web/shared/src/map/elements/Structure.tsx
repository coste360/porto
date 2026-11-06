import { memo } from 'react';

import type { Structure as StructureEl } from '../../api/schema';
import { boundsOf, svgPoints, type Pt } from '../geometry';
import { bandOutline, isBand, structurePoints } from '../scene';
import { MAP_TOKENS, elementClass, type ElementState } from '../tokens';
import { ElementShell, type Lod } from './common';

export type StructureProps = {
  refId: string;
  el: StructureEl;
  state: ElementState;
  /** Unused: the name label follows labelFontCm, the outline is drawn at every level. */
  lod: Lod;
  interactive: boolean;
  label: string;
  /** Font of the name label in cm (structureLabelFontCm); undefined hides it. */
  labelFontCm?: number;
};

/** Nominal font (cm) of a structure name, from its width. */
export function structureNominalFontCm(el: Pick<StructureEl, 'width_cm'>): number {
  return Math.max(80, (el.width_cm ?? 200) * 0.6);
}

/**
 * Where the name of a structure goes: the centre of a closed outline, otherwise the point halfway along the
 * polyline. Names stay horizontal: they are short codes, and upright text reads best at every zoom.
 */
export function structureLabelPlacement(el: StructureEl, pts: readonly Pt[]): Pt | null {
  if (pts.length === 0) return null;
  if (el.closed || pts.length < 2 || el.kind === 'finger') {
    const b = boundsOf(pts);
    return { x: (b.minX + b.maxX) / 2, y: (b.minY + b.maxY) / 2 };
  }
  let total = 0;
  for (let i = 1; i < pts.length; i++)
    total += Math.hypot(pts[i]!.x - pts[i - 1]!.x, pts[i]!.y - pts[i - 1]!.y);
  let rest = total / 2;
  for (let i = 1; i < pts.length; i++) {
    const a = pts[i - 1]!;
    const b = pts[i]!;
    const len = Math.hypot(b.x - a.x, b.y - a.y);
    if (len > 0 && (rest <= len || i === pts.length - 1)) {
      const t = Math.min(1, rest / len);
      return { x: a.x + (b.x - a.x) * t, y: a.y + (b.y - a.y) * t };
    }
    rest -= len;
  }
  return pts[0]!;
}

export const StructureElement = memo(function StructureElement({
  refId,
  el,
  state,
  interactive,
  label,
  labelFontCm,
}: StructureProps) {
  const cls = elementClass(MAP_TOKENS.structure[el.kind], state);
  const pts = structurePoints(el);
  let shape;
  if (el.kind === 'finger' || el.closed) {
    shape = (
      <polygon
        points={svgPoints(pts)}
        className={cls}
        strokeWidth={1}
        vectorEffect="non-scaling-stroke"
      />
    );
  } else if (isBand(el)) {
    shape = (
      <polygon
        points={svgPoints(bandOutline(pts, el.width_cm ?? 0))}
        className={cls}
        strokeWidth={1}
        vectorEffect="non-scaling-stroke"
      />
    );
  } else {
    shape = (
      <polyline
        points={svgPoints(pts)}
        className={`${cls} fill-none`}
        strokeWidth={state === 'normal' ? 3 : 4}
        strokeLinecap="round"
        strokeLinejoin="round"
        vectorEffect="non-scaling-stroke"
      />
    );
  }
  const placement =
    labelFontCm !== undefined && el.kind !== 'finger' ? structureLabelPlacement(el, pts) : null;
  return (
    <ElementShell
      refId={refId}
      kind="structure"
      state={state}
      interactive={interactive}
      label={label}
    >
      {shape}
      {placement && labelFontCm !== undefined ? (
        <text
          x={placement.x}
          y={placement.y}
          fontSize={labelFontCm}
          textAnchor="middle"
          dominantBaseline="central"
          className={MAP_TOKENS.structureLabel}
          strokeWidth={labelFontCm * 0.18}
          strokeLinejoin="round"
          paintOrder="stroke"
          data-testid="structure-label"
        >
          {el.code}
        </text>
      ) : null}
    </ElementShell>
  );
});
