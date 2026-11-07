import { memo } from 'react';

import type { RectGeom } from '../geometry';
import { TOKEN_CLASSES } from '../lenses/tokens';
import type { LensStyle } from '../lenses/types';
import { ElementShell, uprightRotation } from './common';
import { glyphSymbolId } from './Glyphs';

/** Level of detail of a lens-filled element (m3-plan §8.2): fill only, + silhouette, + glyph/badge/name. */
export type LensLod = 'fill' | 'shape' | 'full';

/** M3 thresholds on the on-screen width of the berth (px). */
export const LENS_FILL_PX = 6;
export const LENS_FULL_PX = 14;
/** Text (codes, names) is drawn only at this size or larger (px). */
export const LENS_TEXT_MIN_PX = 9;

export function lensLod(sizeCm: number, pxPerCm: number): LensLod {
  const px = sizeCm * pxPerCm;
  return px < LENS_FILL_PX ? 'fill' : px < LENS_FULL_PX ? 'shape' : 'full';
}

export type LensFillProps = {
  refId: string;
  kind: 'berth' | 'land';
  geom: RectGeom;
  /** Buoys: a circle of this radius around the anchor instead of the rect. */
  buoyRadiusCm?: number;
  code: string;
  lensStyle: LensStyle;
  lod: LensLod;
  pxPerCm: number;
  dimmed: boolean;
  selected: boolean;
  highlighted: boolean;
  /** Schema tile: code (and vessel name) centred, always filled. */
  schema: boolean;
  secondary?: string;
  glyphPrefix: string;
  interactive: boolean;
  label: string;
};

/** A berth or land resource filled with the lens colour, its glyph, badge and code (or a schema tile). */
export const LensFill = memo(function LensFill({
  refId,
  kind,
  geom,
  buoyRadiusCm,
  code,
  lensStyle: style,
  lod,
  pxPerCm,
  dimmed,
  selected,
  highlighted,
  schema,
  secondary,
  glyphPrefix,
  interactive,
  label,
}: LensFillProps) {
  const cls = dimmed ? TOKEN_CLASSES.muted : TOKEN_CLASSES[style.colorToken];
  const ring = selected
    ? 'stroke-ring'
    : highlighted
      ? 'stroke-foreground'
      : lod === 'fill'
        ? 'stroke-none'
        : 'stroke-background';
  const strokeW = selected || highlighted ? 3 : 1;
  const state = selected ? 'selected' : 'normal';
  const textCls = dimmed ? 'fill-muted-foreground' : 'fill-foreground';
  if (buoyRadiusCm !== undefined) {
    const r = buoyRadiusCm;
    return (
      <ElementShell refId={refId} kind={kind} state={state} interactive={interactive} label={label}>
        <circle
          cx={geom.anchor.x}
          cy={geom.anchor.y}
          r={r}
          className={`${cls.fill} ${ring}`}
          strokeWidth={strokeW}
          vectorEffect="non-scaling-stroke"
          data-lens={style.legendKey}
        />
        {lod === 'full' && (r / 3) * pxPerCm >= LENS_TEXT_MIN_PX ? (
          <text
            x={geom.anchor.x}
            y={geom.anchor.y}
            fontSize={r / 3}
            textAnchor="middle"
            dominantBaseline="central"
            className={textCls}
          >
            {code}
          </text>
        ) : null}
      </ElementShell>
    );
  }
  const w = geom.width_cm;
  const l = geom.length_cm;
  const deg = geom.rotation_dd / 10;
  const font = Math.min(w * 0.42, l * 0.3);
  const glyphSize = Math.min(w * 0.55, l * 0.3);
  const showGlyph = !dimmed && lod === 'full' && !!style.glyph && !schema;
  const showCode = (schema || lod === 'full') && font * pxPerCm >= LENS_TEXT_MIN_PX;
  const showBadge =
    !dimmed && lod === 'full' && !!style.badge && font * 0.7 * pxPerCm >= LENS_TEXT_MIN_PX;
  const upright = uprightRotation(geom.rotation_dd);
  return (
    <ElementShell
      refId={refId}
      kind={kind}
      state={state}
      interactive={interactive}
      label={label}
      transform={`translate(${geom.anchor.x} ${geom.anchor.y}) rotate(${deg})`}
    >
      <rect
        x={0}
        y={-w / 2}
        width={l}
        height={w}
        className={`${cls.fill} ${ring}`}
        strokeWidth={strokeW}
        vectorEffect="non-scaling-stroke"
        data-lens={style.legendKey}
      />
      {showGlyph ? (
        <use
          href={`#${glyphSymbolId(glyphPrefix, style.glyph!)}`}
          x={l - glyphSize * 1.1}
          y={-glyphSize / 2}
          width={glyphSize}
          height={glyphSize}
          className="fill-background"
          transform={`rotate(${upright} ${l - glyphSize * 0.6} 0)`}
        />
      ) : null}
      {showCode ? (
        <text
          x={schema ? l / 2 : glyphSize * 0.2 + font}
          y={schema && secondary ? -font * 0.35 : 0}
          fontSize={font}
          textAnchor="middle"
          dominantBaseline="central"
          transform={`rotate(${upright} ${schema ? l / 2 : glyphSize * 0.2 + font} 0)`}
          className={`${textCls} font-semibold`}
        >
          {code}
        </text>
      ) : null}
      {schema && secondary && font * 0.6 * pxPerCm >= LENS_TEXT_MIN_PX ? (
        <text
          x={l / 2}
          y={font * 0.45}
          fontSize={font * 0.6}
          textAnchor="middle"
          dominantBaseline="central"
          transform={`rotate(${upright} ${l / 2} 0)`}
          className={textCls}
        >
          {secondary}
        </text>
      ) : null}
      {showBadge ? (
        <text
          x={l / 2}
          y={w / 2 - font * 0.45}
          fontSize={font * 0.7}
          textAnchor="middle"
          dominantBaseline="central"
          transform={`rotate(${upright} ${l / 2} ${w / 2 - font * 0.45})`}
          className={textCls}
          data-badge
        >
          {style.badge}
        </text>
      ) : null}
    </ElementShell>
  );
});
