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
  /** The glyph is drawn by LensGlyph above the vessels (HarborMap), not here. */
  glyphOnTop?: boolean;
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
  glyphOnTop = false,
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
  // Codes and glyphs contrast with the fill they sit on (theme tokens, both themes).
  const textCls = dimmed ? 'fill-muted-foreground' : cls.onFill;
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
  const glyphSize = lensGlyphSize(geom);
  const showGlyph = !glyphOnTop && lensGlyphShown(style, lod, dimmed, schema);
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
        <GlyphUse geom={geom} glyph={style.glyph!} prefix={glyphPrefix} cls={cls.onFill} />
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

/** Size (cm) of the lens glyph of a rect: inside the berth whatever its proportions. */
export function lensGlyphSize(geom: Pick<RectGeom, 'width_cm' | 'length_cm'>): number {
  return Math.min(geom.width_cm * 0.55, geom.length_cm * 0.3);
}

/** Whether a lens glyph is drawn for a rect (full level of detail, not dimmed, scale mode). */
export function lensGlyphShown(
  style: LensStyle,
  lod: LensLod,
  dimmed: boolean,
  schema: boolean,
): boolean {
  return !dimmed && lod === 'full' && !!style.glyph && !schema;
}

/** The glyph near the outer end of the rect, inside it with a margin, upright (berth-local coordinates). */
function GlyphUse({
  geom,
  glyph,
  prefix,
  cls,
}: {
  geom: RectGeom;
  glyph: NonNullable<LensStyle['glyph']>;
  prefix: string;
  cls: string;
}) {
  const l = geom.length_cm;
  const g = lensGlyphSize(geom);
  const cx = l - g * 0.75;
  return (
    <use
      href={`#${glyphSymbolId(prefix, glyph)}`}
      x={cx - g / 2}
      y={-g / 2}
      width={g}
      height={g}
      className={cls}
      transform={`rotate(${uprightRotation(geom.rotation_dd)} ${cx} 0)`}
    />
  );
}

/**
 * The lens glyph of a berth drawn above the vessel silhouettes (HarborMap glyph layer), so a boat filling its
 * berth never hides the star or the arrow. Not interactive: the berth below takes the pointer.
 */
export const LensGlyph = memo(function LensGlyph({
  geom,
  lensStyle,
  glyphPrefix,
}: {
  geom: RectGeom;
  lensStyle: LensStyle;
  glyphPrefix: string;
}) {
  if (!lensStyle.glyph) return null;
  return (
    <g
      transform={`translate(${geom.anchor.x} ${geom.anchor.y}) rotate(${geom.rotation_dd / 10})`}
      className="pointer-events-none"
      aria-hidden
      data-glyph={lensStyle.glyph}
    >
      <GlyphUse
        geom={geom}
        glyph={lensStyle.glyph}
        prefix={glyphPrefix}
        cls={TOKEN_CLASSES[lensStyle.colorToken].onFill}
      />
    </g>
  );
});
