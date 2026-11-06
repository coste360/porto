import { memo } from 'react';

import type { MapBackground } from '../../api/schema';

/** Background image size in map cm from its calibration (milli-cm per pixel). Floats, render only. */
export function backgroundSizeCm(
  bg: Pick<MapBackground, 'width_px' | 'height_px' | 'scale_mcm_per_px'>,
) {
  return {
    w: (bg.width_px * bg.scale_mcm_per_px) / 1000,
    h: (bg.height_px * bg.scale_mcm_per_px) / 1000,
  };
}

/**
 * The calibrated background, drawn only through `<image>` (SVG backgrounds included: no scripts run in an
 * image context). Top-left at `origin`, rotated around it.
 */
export const Background = memo(function Background({
  bg,
  href,
}: {
  bg: MapBackground;
  href: string;
}) {
  if (!bg.visible) return null;
  const { w, h } = backgroundSizeCm(bg);
  return (
    <image
      href={href}
      x={bg.origin.x}
      y={bg.origin.y}
      width={w}
      height={h}
      preserveAspectRatio="none"
      opacity={bg.opacity_pct / 100}
      transform={`rotate(${bg.rotation_dd / 10} ${bg.origin.x} ${bg.origin.y})`}
      aria-hidden="true"
    />
  );
});
