import { useElementSize } from './useElementSize';

// Heights that fill the rest of the viewport without arbitrary Tailwind values: the height is rounded down to a
// step of the spacing scale (h-N = N x 0.25rem). shared/src/styles/globals.css safelists the classes with
// `@source inline(...)` because they are built here, not written in full in the sources.

/** Spacing units (4 px each) of the smallest, largest and step height. */
export const FILL_MIN_UNITS = 64;
export const FILL_MAX_UNITS = 400;
export const FILL_STEP_UNITS = 8;

/**
 * Spacing units of a panel whose top is `top` px from the top of the document in a `viewport` px high window,
 * leaving `reserve` px below it (padding, a legend, a fixed tab bar); never below `minUnits` and never above
 * FILL_MAX_UNITS; null while nothing has been measured.
 */
export function fillUnits(
  viewport: number,
  top: number,
  reserve: number,
  minUnits: number = FILL_MIN_UNITS,
): number | null {
  if (viewport <= 0) return null;
  const px = viewport - top - reserve;
  const units = Math.floor(px / 4 / FILL_STEP_UNITS) * FILL_STEP_UNITS;
  return Math.min(FILL_MAX_UNITS, Math.max(minUnits, units));
}

/** Tailwind class of a height in spacing units, with an optional variant (`lg:`). */
export function heightClass(units: number, variant: '' | 'lg:' = ''): string {
  return `${variant}h-${units}`;
}

/**
 * A callback ref and the height class that makes the element fill the viewport below its top (`reserve` px left
 * free), or `fallback` until the element is measured.
 */
export function useFillHeight<T extends HTMLElement>(opts: {
  reserve: number;
  minUnits?: number;
  variant?: '' | 'lg:';
  fallback: string;
}): [(el: T | null) => void, string] {
  const [ref, size] = useElementSize<T>();
  const units = fillUnits(size.viewport, size.top, opts.reserve, opts.minUnits);
  return [ref, units === null ? opts.fallback : heightClass(units, opts.variant ?? '')];
}
