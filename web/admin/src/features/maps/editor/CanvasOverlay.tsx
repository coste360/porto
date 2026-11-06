import { useT } from '@porto/shared/i18n';
import { formatCm } from '@porto/shared/lib/format';
import { MAP_TOKENS, direction, svgPoints, type Box, type Pt } from '@porto/shared/map';

import { usePointer, type PointerStore } from './model/pointerStore';
import type { SnapResult } from './model/snap';

export type CanvasOverlayProps = {
  store: PointerStore;
  pxPerCm: number;
  /** Points collected by the active tool or pick. */
  pending: readonly SnapResult[];
  /** Whether the active tool shows the snapped cursor. */
  drawing: boolean;
  selectionBox: Box | null;
  rotateHandle: boolean;
  measure: [Pt, Pt] | null;
  calibration: [Pt, Pt] | null;
};

/** Editor overlay in map coordinates; sizes are divided by px/cm so they stay constant on screen. */
export function CanvasOverlay(p: CanvasOverlayProps) {
  const { i18n } = useT();
  const { hover, cursor, marquee } = usePointer(p.store);
  const px = (n: number) => n / p.pxPerCm;
  const live = p.drawing ? (hover?.point ?? cursor) : null;
  const pendingPts = p.pending.map((s) => s.point);
  const handle = p.selectionBox
    ? { x: (p.selectionBox.minX + p.selectionBox.maxX) / 2, y: p.selectionBox.minY - px(28) }
    : null;
  return (
    <g data-layer="overlay">
      {p.selectionBox ? (
        <rect
          x={p.selectionBox.minX}
          y={p.selectionBox.minY}
          width={p.selectionBox.maxX - p.selectionBox.minX}
          height={p.selectionBox.maxY - p.selectionBox.minY}
          className="fill-none stroke-ring"
          strokeWidth={1}
          strokeDasharray="4 4"
          vectorEffect="non-scaling-stroke"
          pointerEvents="none"
        />
      ) : null}
      {p.rotateHandle && handle && p.selectionBox ? (
        <g data-handle="rotate" className="cursor-grab">
          <line
            x1={handle.x}
            y1={handle.y}
            x2={handle.x}
            y2={p.selectionBox.minY}
            className={MAP_TOKENS.guide}
            strokeWidth={1}
            vectorEffect="non-scaling-stroke"
          />
          <circle
            cx={handle.x}
            cy={handle.y}
            r={px(7)}
            className={MAP_TOKENS.handle}
            strokeWidth={2}
            vectorEffect="non-scaling-stroke"
            data-handle="rotate"
          />
        </g>
      ) : null}
      {pendingPts.length > 0 ? (
        <polyline
          points={svgPoints(live ? [...pendingPts, live] : pendingPts)}
          className={`fill-none ${MAP_TOKENS.guide}`}
          strokeWidth={2}
          strokeDasharray="6 4"
          vectorEffect="non-scaling-stroke"
          pointerEvents="none"
        />
      ) : null}
      {pendingPts.map((pt, i) => (
        <circle
          key={i}
          cx={pt.x}
          cy={pt.y}
          r={px(4)}
          className={MAP_TOKENS.handle}
          strokeWidth={2}
          vectorEffect="non-scaling-stroke"
          pointerEvents="none"
        />
      ))}
      {p.drawing && hover ? (
        <g pointerEvents="none" data-snap={hover.kind}>
          {hover.guides.map((g, i) => (
            <line
              key={i}
              x1={g.a.x}
              y1={g.a.y}
              x2={g.b.x}
              y2={g.b.y}
              className={MAP_TOKENS.guide}
              strokeWidth={1}
              strokeDasharray="3 3"
              vectorEffect="non-scaling-stroke"
            />
          ))}
          <circle
            cx={hover.point.x}
            cy={hover.point.y}
            r={px(hover.kind === 'none' ? 3 : 6)}
            className={
              hover.kind === 'edge' || hover.kind === 'vertex'
                ? 'fill-chart-4 stroke-background'
                : MAP_TOKENS.handle
            }
            strokeWidth={1.5}
            vectorEffect="non-scaling-stroke"
          />
          {hover.kind === 'edge' && hover.rotation_dd !== undefined ? (
            <line
              x1={hover.point.x}
              y1={hover.point.y}
              x2={hover.point.x + direction(hover.rotation_dd).x * px(30)}
              y2={hover.point.y + direction(hover.rotation_dd).y * px(30)}
              className={MAP_TOKENS.guide}
              strokeWidth={2}
              vectorEffect="non-scaling-stroke"
            />
          ) : null}
        </g>
      ) : null}
      {marquee ? (
        <rect
          x={marquee.minX}
          y={marquee.minY}
          width={marquee.maxX - marquee.minX}
          height={marquee.maxY - marquee.minY}
          className={MAP_TOKENS.marquee}
          strokeWidth={1}
          vectorEffect="non-scaling-stroke"
          pointerEvents="none"
        />
      ) : null}
      {[p.measure, p.calibration].map((seg, i) =>
        seg ? (
          <g key={i} pointerEvents="none" data-testid={i === 0 ? 'measure' : 'calibration'}>
            <line
              x1={seg[0].x}
              y1={seg[0].y}
              x2={seg[1].x}
              y2={seg[1].y}
              className={MAP_TOKENS.measure}
              strokeWidth={2}
              vectorEffect="non-scaling-stroke"
            />
            <text
              x={(seg[0].x + seg[1].x) / 2}
              y={(seg[0].y + seg[1].y) / 2 - px(8)}
              fontSize={px(13)}
              textAnchor="middle"
              className={MAP_TOKENS.label}
            >
              {formatCm(
                Math.round(Math.hypot(seg[1].x - seg[0].x, seg[1].y - seg[0].y)),
                i18n.language,
              )}
            </text>
          </g>
        ) : null,
      )}
    </g>
  );
}

/** Cursor coordinates in metres for the status bar. */
export function CursorReadout({ store }: { store: PointerStore }) {
  const { t, i18n } = useT();
  const { cursor } = usePointer(store);
  return (
    <span data-testid="cursor-readout">
      {cursor
        ? t('editor:status.cursor', {
            x: formatCm(Math.round(cursor.x), i18n.language),
            y: formatCm(Math.round(cursor.y), i18n.language),
          })
        : t('editor:status.noCursor')}
    </span>
  );
}
