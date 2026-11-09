import { useCallback, useEffect, useState } from 'react';

export type ElementSize = { width: number; height: number; top: number; viewport: number };

/**
 * The size of an element, kept up to date by a ResizeObserver and by window resizes (`top` is the element's
 * distance from the top of the document and `viewport` the window height, for layouts that fill the rest of the
 * viewport). Returns a callback ref;
 * the size stays zero where ResizeObserver is missing (jsdom).
 */
export function useElementSize<T extends HTMLElement>(): [(el: T | null) => void, ElementSize] {
  const [el, setEl] = useState<T | null>(null);
  const [size, setSize] = useState<ElementSize>({ width: 0, height: 0, top: 0, viewport: 0 });
  const ref = useCallback((node: T | null) => setEl(node), []);
  useEffect(() => {
    if (!el || typeof ResizeObserver === 'undefined') return;
    const measure = () => {
      const r = el.getBoundingClientRect();
      const top = Math.round(r.top + window.scrollY);
      const next = {
        width: Math.round(r.width),
        height: Math.round(r.height),
        top,
        viewport: window.innerHeight,
      };
      setSize((s) =>
        s.width === next.width &&
        s.height === next.height &&
        s.top === next.top &&
        s.viewport === next.viewport
          ? s
          : next,
      );
    };
    measure();
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    window.addEventListener('resize', measure);
    return () => {
      ro.disconnect();
      window.removeEventListener('resize', measure);
    };
  }, [el]);
  return [ref, size];
}
