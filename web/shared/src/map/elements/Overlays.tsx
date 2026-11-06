import { formatCm } from '../geometry';
import { MAP_TOKENS } from '../tokens';
import { pxPerCm, type Viewport } from '../viewport';

/** Grid lines every `grid` cm inside the map, only when they are at least 8 px apart. */
export function Grid({
  id,
  grid,
  width,
  height,
  vp,
}: {
  id: string;
  grid: number;
  width: number;
  height: number;
  vp: Viewport;
}) {
  if (grid <= 0 || grid * pxPerCm(vp) < 8) return null;
  return (
    <g aria-hidden="true">
      <defs>
        <pattern id={id} width={grid} height={grid} patternUnits="userSpaceOnUse">
          <path
            d={`M ${grid} 0 L 0 0 0 ${grid}`}
            className={`fill-none ${MAP_TOKENS.grid}`}
            strokeWidth={0.5}
            vectorEffect="non-scaling-stroke"
          />
        </pattern>
      </defs>
      <rect x={0} y={0} width={width} height={height} fill={`url(#${id})`} />
    </g>
  );
}

const NICE = [1, 2, 5];

/** Length (cm) of a scale bar about `targetPx` long: 1, 2 or 5 × 10^k. */
export function niceScaleLength(vp: Viewport, targetPx = 120): number {
  const raw = targetPx / pxPerCm(vp);
  const exp = Math.floor(Math.log10(Math.max(1, raw)));
  let best = 10 ** exp;
  for (const n of NICE) {
    const v = n * 10 ** exp;
    if (v <= raw) best = v;
  }
  return Math.max(1, Math.round(best));
}

/** Scale bar fixed to the bottom-left corner of the view, labelled in metres. */
export function ScaleBar({ vp, locale }: { vp: Viewport; locale: string }) {
  const s = pxPerCm(vp);
  const len = niceScaleLength(vp);
  const x = vp.x + 16 / s;
  const y = vp.y + vp.h - 20 / s;
  const h = 6 / s;
  return (
    <g aria-hidden="true" className={MAP_TOKENS.scaleBar} data-testid="map-scale-bar">
      <rect x={x} y={y} width={len / 2} height={h} />
      <rect
        x={x + len / 2}
        y={y}
        width={len / 2}
        height={h}
        className="fill-background"
        strokeWidth={1}
        vectorEffect="non-scaling-stroke"
      />
      <text x={x + len} y={y - 4 / s} fontSize={11 / s} textAnchor="end">
        {formatCm(len, locale)}
      </text>
    </g>
  );
}

/** North arrow fixed to the top-right corner of the view (the map is drawn north up). */
export function NorthArrow({ vp }: { vp: Viewport }) {
  const s = pxPerCm(vp);
  const size = 28 / s;
  const cx = vp.x + vp.w - 24 / s;
  const cy = vp.y + 30 / s;
  return (
    <g aria-hidden="true" className={MAP_TOKENS.scaleBar}>
      <polygon
        points={`${cx},${cy - size / 2} ${cx + size / 4},${cy + size / 2} ${cx},${cy + size / 4} ${cx - size / 4},${cy + size / 2}`}
      />
      <text x={cx} y={cy - size / 2 - 3 / s} fontSize={11 / s} textAnchor="middle">
        N
      </text>
    </g>
  );
}
