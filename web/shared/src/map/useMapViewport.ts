import { useCallback, useEffect, useRef, useState, type PointerEvent } from 'react';

import type { Box, Pt } from './geometry';
import { fitToView, panBy, resize, screenToMap, zoomAt, type Viewport } from './viewport';

const DEFAULT_SIZE = { w: 800, h: 600 };
const WHEEL_STEP = 1.0015;

export type MapViewport = {
  viewport: Viewport;
  setViewport: (update: (vp: Viewport) => Viewport) => void;
  /** Callback ref of the element whose size is the screen size of the map. */
  bindContainer: (el: HTMLElement | null) => void;
  /** Callback ref of the `<svg>` (wheel zoom). */
  bindSvg: (el: SVGSVGElement | null) => void;
  fit: () => void;
  zoomBy: (factor: number) => void;
  /** Screen point (relative to the svg) of a pointer event. */
  screenPoint: (e: { clientX: number; clientY: number }) => Pt;
  /** Map point (cm, floats) of a pointer event. */
  mapPoint: (e: { clientX: number; clientY: number }) => Pt;
  /** Pointer handlers for drag-to-pan (when `canPan` says so) and two-finger pinch zoom. */
  gestures: {
    onPointerDown: (e: PointerEvent<SVGSVGElement>, canPan: boolean) => boolean;
    onPointerMove: (e: PointerEvent<SVGSVGElement>) => boolean;
    onPointerUp: (e: PointerEvent<SVGSVGElement>) => boolean;
  };
};

/**
 * Pan and zoom state of a map (m1-plan §5.2): the viewBox follows the container size, wheel zooms at the
 * cursor, a drag pans, two pointers pinch. Pan and zoom change only the viewBox.
 */
export function useMapViewport(content: Box): MapViewport {
  const [state, setState] = useState<Viewport | null>(null);
  const [container, setContainer] = useState<HTMLElement | null>(null);
  const [svg, setSvg] = useState<SVGSVGElement | null>(null);
  const contentRef = useRef(content);
  useEffect(() => {
    contentRef.current = content;
  }, [content]);

  const viewport = state ?? fitToView(content, DEFAULT_SIZE.w, DEFAULT_SIZE.h);

  const setViewport = useCallback(
    (update: (vp: Viewport) => Viewport) =>
      setState((prev) =>
        update(prev ?? fitToView(contentRef.current, DEFAULT_SIZE.w, DEFAULT_SIZE.h)),
      ),
    [],
  );

  useEffect(() => {
    if (!container || typeof ResizeObserver === 'undefined') return;
    const ro = new ResizeObserver((entries) => {
      const rect = entries[0]?.contentRect;
      if (!rect || rect.width === 0 || rect.height === 0) return;
      setState((prev) =>
        prev
          ? resize(prev, rect.width, rect.height)
          : fitToView(contentRef.current, rect.width, rect.height),
      );
    });
    ro.observe(container);
    return () => ro.disconnect();
  }, [container]);

  const screenPoint = useCallback(
    (e: { clientX: number; clientY: number }): Pt => {
      const r = svg?.getBoundingClientRect();
      return { x: e.clientX - (r?.left ?? 0), y: e.clientY - (r?.top ?? 0) };
    },
    [svg],
  );

  useEffect(() => {
    if (!svg) return;
    const onWheel = (e: WheelEvent) => {
      e.preventDefault();
      const r = svg.getBoundingClientRect();
      const at = { x: e.clientX - r.left, y: e.clientY - r.top };
      const factor = WHEEL_STEP ** -e.deltaY;
      setViewport((vp) => zoomAt(vp, factor, at));
    };
    svg.addEventListener('wheel', onWheel, { passive: false });
    return () => svg.removeEventListener('wheel', onWheel);
  }, [svg, setViewport]);

  const pointers = useRef(new Map<number, Pt>());
  const panning = useRef(false);

  const gestures = {
    onPointerDown: (e: PointerEvent<SVGSVGElement>, canPan: boolean) => {
      pointers.current.set(e.pointerId, { x: e.clientX, y: e.clientY });
      if (pointers.current.size >= 2 || canPan || e.button === 1) {
        panning.current = true;
        e.currentTarget.setPointerCapture?.(e.pointerId);
        return true;
      }
      return false;
    },
    onPointerMove: (e: PointerEvent<SVGSVGElement>) => {
      const prev = pointers.current.get(e.pointerId);
      if (!prev || !panning.current) return false;
      const next = { x: e.clientX, y: e.clientY };
      if (pointers.current.size >= 2) {
        const others = [...pointers.current.entries()].filter(([id]) => id !== e.pointerId);
        const other = others[0]?.[1];
        if (other) {
          const before = Math.hypot(prev.x - other.x, prev.y - other.y);
          const after = Math.hypot(next.x - other.x, next.y - other.y);
          const mid = screenPoint({
            clientX: (next.x + other.x) / 2,
            clientY: (next.y + other.y) / 2,
          });
          if (before > 0) setViewport((vp) => zoomAt(vp, after / before, mid));
        }
      } else {
        setViewport((vp) => panBy(vp, next.x - prev.x, next.y - prev.y));
      }
      pointers.current.set(e.pointerId, next);
      return true;
    },
    onPointerUp: (e: PointerEvent<SVGSVGElement>) => {
      pointers.current.delete(e.pointerId);
      const was = panning.current;
      if (pointers.current.size === 0) panning.current = false;
      return was;
    },
  };

  return {
    viewport,
    setViewport,
    bindContainer: setContainer,
    bindSvg: setSvg,
    fit: useCallback(
      () => setViewport((vp) => fitToView(contentRef.current, vp.screenW, vp.screenH)),
      [setViewport],
    ),
    zoomBy: useCallback(
      (factor: number) =>
        setViewport((vp) => zoomAt(vp, factor, { x: vp.screenW / 2, y: vp.screenH / 2 })),
      [setViewport],
    ),
    screenPoint,
    mapPoint: (e) => screenToMap(viewport, screenPoint(e)),
    gestures,
  };
}
