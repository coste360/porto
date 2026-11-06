import type { ReactNode } from 'react';

import type { ElementState } from '../tokens';

/** Level of detail of an element: plain filled shape, outlined shape, or shape with labels. */
export type Lod = 'simple' | 'shape' | 'labelled';

/** Below this on-screen size (px) a berth is a filled rect without stroke, labels or glyphs. */
export const LOD_SIMPLE_PX = 6;
/** A code label (berth number, depth, free text) is drawn only when its font is at least this many pixels. */
export const LABEL_MIN_PX = 10;
/**
 * Screen size (px) below which a structure name never shrinks: structure labels grow in map units as the map
 * zooms out, so piers and quays stay readable at "fit to view" (ADR 0004, level of detail).
 */
export const STRUCTURE_LABEL_PX = 13;

/** Font of a structure label in cm: its nominal size in the map, never smaller than STRUCTURE_LABEL_PX. */
export function structureLabelFontCm(nominalCm: number, pxPerCm: number): number {
  return pxPerCm > 0 ? Math.max(nominalCm, STRUCTURE_LABEL_PX / pxPerCm) : nominalCm;
}

export function lodFor(sizeCm: number, labelFontCm: number, pxPerCm: number, labels: boolean): Lod {
  if (sizeCm * pxPerCm < LOD_SIMPLE_PX) return 'simple';
  if (labels && labelFontCm * pxPerCm >= LABEL_MIN_PX) return 'labelled';
  return 'shape';
}

export type ElementShellProps = {
  refId: string;
  kind: string;
  state: ElementState;
  interactive: boolean;
  label: string;
  transform?: string;
  children: ReactNode;
};

/**
 * Wrapper of every element: `data-ref` for event delegation in HarborMap and, when interactive, a focusable
 * `role="button"` with an accessible name.
 */
export function ElementShell({
  refId,
  kind,
  state,
  interactive,
  label,
  transform,
  children,
}: ElementShellProps) {
  return (
    <g
      data-ref={refId}
      data-kind={kind}
      data-state={state}
      transform={transform}
      {...(interactive
        ? {
            role: 'button',
            tabIndex: 0,
            'aria-label': label,
            'aria-pressed': state === 'selected',
            className: 'cursor-pointer outline-none focus-visible:opacity-80',
          }
        : { 'aria-hidden': true })}
    >
      {children}
    </g>
  );
}

/** Text upright along a rotated element: flips by 180° when the element points left. */
export function uprightRotation(rotationDd: number): number {
  const deg = rotationDd / 10;
  return deg > 90 && deg < 270 ? 180 : 0;
}
