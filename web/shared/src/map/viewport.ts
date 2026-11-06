import type { Box, Pt } from './geometry';

/**
 * Pan and zoom state: the SVG viewBox in map cm, with the same aspect ratio as the screen, so that one factor
 * (`pxPerCm`) converts between screen pixels and map cm. Floats, render only.
 */
export type Viewport = {
  x: number;
  y: number;
  w: number;
  h: number;
  /** Size of the SVG element in CSS pixels. */
  screenW: number;
  screenH: number;
};

export const MIN_PX_PER_CM = 0.0005;
export const MAX_PX_PER_CM = 20;

export function pxPerCm(vp: Viewport): number {
  return vp.w > 0 ? vp.screenW / vp.w : 1;
}

/** Viewport that shows `content` centred with a margin (fraction of the size). */
export function fitToView(content: Box, screenW: number, screenH: number, margin = 0.05): Viewport {
  const sw = Math.max(1, screenW);
  const sh = Math.max(1, screenH);
  const cw = Math.max(1, content.maxX - content.minX) * (1 + 2 * margin);
  const ch = Math.max(1, content.maxY - content.minY) * (1 + 2 * margin);
  const scale = Math.min(sw / cw, sh / ch);
  const w = sw / scale;
  const h = sh / scale;
  const cx = (content.minX + content.maxX) / 2;
  const cy = (content.minY + content.maxY) / 2;
  return { x: cx - w / 2, y: cy - h / 2, w, h, screenW: sw, screenH: sh };
}

/** Keeps the centre and the scale when the screen size changes. */
export function resize(vp: Viewport, screenW: number, screenH: number): Viewport {
  const s = pxPerCm(vp);
  const cx = vp.x + vp.w / 2;
  const cy = vp.y + vp.h / 2;
  const w = Math.max(1, screenW) / s;
  const h = Math.max(1, screenH) / s;
  return {
    x: cx - w / 2,
    y: cy - h / 2,
    w,
    h,
    screenW: Math.max(1, screenW),
    screenH: Math.max(1, screenH),
  };
}

export function screenToMap(vp: Viewport, p: Pt): Pt {
  const s = pxPerCm(vp);
  return { x: vp.x + p.x / s, y: vp.y + p.y / s };
}

export function mapToScreen(vp: Viewport, p: Pt): Pt {
  const s = pxPerCm(vp);
  return { x: (p.x - vp.x) * s, y: (p.y - vp.y) * s };
}

/** Zooms by `factor` (> 1 zooms in) keeping the map point under the screen point `at` fixed. */
export function zoomAt(vp: Viewport, factor: number, at: Pt): Viewport {
  const s = pxPerCm(vp);
  const next = Math.min(MAX_PX_PER_CM, Math.max(MIN_PX_PER_CM, s * factor));
  const anchor = screenToMap(vp, at);
  const w = vp.screenW / next;
  const h = vp.screenH / next;
  return { ...vp, x: anchor.x - at.x / next, y: anchor.y - at.y / next, w, h };
}

/** Pans by a screen delta in pixels. */
export function panBy(vp: Viewport, dxPx: number, dyPx: number): Viewport {
  const s = pxPerCm(vp);
  return { ...vp, x: vp.x - dxPx / s, y: vp.y - dyPx / s };
}

/** Centres the view on a box, zooming so that it fills about a third of the screen. */
export function focusBox(vp: Viewport, box: Box): Viewport {
  const fitted = fitToView(box, vp.screenW, vp.screenH, 1);
  return pxPerCm(fitted) > MAX_PX_PER_CM ? { ...vp, ...centreOn(vp, box) } : fitted;
}

function centreOn(vp: Viewport, box: Box): Pick<Viewport, 'x' | 'y'> {
  return { x: (box.minX + box.maxX) / 2 - vp.w / 2, y: (box.minY + box.maxY) / 2 - vp.h / 2 };
}

export function viewBoxOf(vp: Viewport): Box {
  return { minX: vp.x, minY: vp.y, maxX: vp.x + vp.w, maxY: vp.y + vp.h };
}

export function viewBoxAttr(vp: Viewport): string {
  return `${vp.x} ${vp.y} ${vp.w} ${vp.h}`;
}

/** Zoom as a percentage of the fitted view. */
export function zoomPercent(vp: Viewport, fitted: Viewport): number {
  return Math.round((pxPerCm(vp) / pxPerCm(fitted)) * 100);
}
