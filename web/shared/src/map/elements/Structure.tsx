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
  /** Unused: the outline is drawn at every level; the name is a StructureLabel in the label layer. */
  lod: Lod;
  interactive: boolean;
  label: string;
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

/** Width of an upper-case code in em (generous: the codes are capitals of a humanist sans). */
const CODE_EM = 0.68;

export type StructureLabelFit = {
  font: number;
  /** Read bottom to top: a tall closed outline (a breakwater) too narrow for the code across it. */
  rotate: boolean;
  /** Background pill of a label drawn over a line (quays, piers), so the code never sits on the stroke. */
  pill?: { w: number; h: number };
};

/**
 * Font and frame of a structure name: inside a closed outline it shrinks (and turns along a tall outline) so the
 * code never leaves the box; on a line (quay, pier) it sits on a background pill so lines never cross it. Pure.
 */
export function structureLabelFit(
  el: Pick<StructureEl, 'closed' | 'code' | 'kind'>,
  pts: readonly Pt[],
  fontCm: number,
): StructureLabelFit {
  const n = Math.max(1, el.code.length);
  if (el.closed && pts.length >= 3) {
    const b = boundsOf(pts);
    const w = b.maxX - b.minX;
    const h = b.maxY - b.minY;
    const across = Math.min(fontCm, (0.9 * w) / (n * CODE_EM), 0.8 * h);
    const along = Math.min(fontCm, (0.9 * h) / (n * CODE_EM), 0.8 * w);
    return along > across * 1.2 && h > w
      ? { font: along, rotate: true }
      : { font: across, rotate: false };
  }
  return {
    font: fontCm,
    rotate: false,
    pill: { w: (n * CODE_EM + 0.6) * fontCm, h: 1.35 * fontCm },
  };
}

export const StructureElement = memo(function StructureElement({
  refId,
  el,
  state,
  interactive,
  label,
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
  return (
    <ElementShell
      refId={refId}
      kind="structure"
      state={state}
      interactive={interactive}
      label={label}
    >
      {shape}
    </ElementShell>
  );
});

/**
 * The name of a structure, drawn by HarborMap in the label layer above berths, boats and glyphs so nothing covers
 * it (fontCm from structureLabelFontCm). Not interactive: the structure below takes the pointer.
 */
export const StructureLabel = memo(function StructureLabel({
  el,
  fontCm,
}: {
  el: StructureEl;
  fontCm: number;
}) {
  if (el.kind === 'finger') return null;
  const pts = structurePoints(el);
  const placement = structureLabelPlacement(el, pts);
  if (!placement) return null;
  const fit = structureLabelFit(el, pts, fontCm);
  return (
    <g className="pointer-events-none" aria-hidden>
      <g transform={fit.rotate ? `rotate(-90 ${placement.x} ${placement.y})` : undefined}>
        {fit.pill ? (
          <rect
            x={placement.x - fit.pill.w / 2}
            y={placement.y - fit.pill.h / 2}
            width={fit.pill.w}
            height={fit.pill.h}
            rx={fit.pill.h / 4}
            className={MAP_TOKENS.structureLabelPill}
            strokeWidth={1}
            vectorEffect="non-scaling-stroke"
          />
        ) : null}
        <text
          x={placement.x}
          y={placement.y}
          fontSize={fit.font}
          textAnchor="middle"
          dominantBaseline="central"
          className={MAP_TOKENS.structureLabel}
          strokeWidth={fit.font * 0.18}
          strokeLinejoin="round"
          paintOrder="stroke"
          data-testid="structure-label"
        >
          {el.code}
        </text>
      </g>
    </g>
  );
});
