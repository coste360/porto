import { memo } from 'react';

import type { Berth as BerthEl, LandResource } from '../../api/schema';
import { isBuoy } from '../scene';
import { MAP_TOKENS, elementClass, type ElementState } from '../tokens';
import { ElementShell, uprightRotation, type Lod } from './common';

export type RectElementProps = {
  refId: string;
  el: BerthEl | LandResource;
  kind: 'berth' | 'land';
  state: ElementState;
  lod: Lod;
  interactive: boolean;
  label: string;
};

/** Label font of a rect element (cm), from its size. */
export function rectLabelFontCm(widthCm: number, lengthCm: number): number {
  return Math.min(widthCm * 0.42, lengthCm * 0.3);
}

/** A berth (rotated rect, or a buoy circle) or a land resource. */
export const RectElement = memo(function RectElement({
  refId,
  el,
  kind,
  state,
  lod,
  interactive,
  label,
}: RectElementProps) {
  const inactive = el.active === false && state === 'normal' ? 'inactive' : state;
  if (kind === 'berth' && isBuoy(el as BerthEl)) {
    const b = el as BerthEl;
    const r = b.swing_radius_cm ?? b.length_cm / 2;
    const cls = elementClass(MAP_TOKENS.buoy, inactive);
    return (
      <ElementShell refId={refId} kind={kind} state={state} interactive={interactive} label={label}>
        <circle
          cx={b.anchor.x}
          cy={b.anchor.y}
          r={r}
          className={cls}
          strokeWidth={1}
          strokeDasharray={lod === 'simple' ? undefined : '4 3'}
          vectorEffect="non-scaling-stroke"
        />
        <circle
          cx={b.anchor.x}
          cy={b.anchor.y}
          r={Math.max(30, r / 12)}
          className={MAP_TOKENS.pedestal}
        />
        {lod === 'labelled' ? (
          <text
            x={b.anchor.x}
            y={b.anchor.y - r / 3}
            fontSize={r / 3}
            textAnchor="middle"
            dominantBaseline="central"
            className={MAP_TOKENS.label}
          >
            {b.code}
          </text>
        ) : null}
      </ElementShell>
    );
  }
  const base = kind === 'berth' ? MAP_TOKENS.berth : MAP_TOKENS.land_resource;
  const w = el.width_cm;
  const l = el.length_cm;
  const deg = el.rotation_dd / 10;
  const font = rectLabelFontCm(w, l);
  return (
    <ElementShell
      refId={refId}
      kind={kind}
      state={state}
      interactive={interactive}
      label={label}
      transform={`translate(${el.anchor.x} ${el.anchor.y}) rotate(${deg})`}
    >
      <rect
        x={0}
        y={-w / 2}
        width={l}
        height={w}
        className={elementClass(base, inactive)}
        strokeWidth={lod === 'simple' ? 0 : 1}
        vectorEffect="non-scaling-stroke"
      />
      {lod === 'labelled' ? (
        <text
          x={l / 2}
          y={0}
          fontSize={font}
          textAnchor="middle"
          dominantBaseline="central"
          transform={`rotate(${uprightRotation(el.rotation_dd)} ${l / 2} 0)`}
          className={MAP_TOKENS.label}
        >
          {el.code}
        </text>
      ) : null}
    </ElementShell>
  );
});
