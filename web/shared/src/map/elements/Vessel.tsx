import { memo } from 'react';

import type { VesselItem } from '../dayView';
import { TOKEN_CLASSES } from '../lenses/tokens';
import type { HarborToken } from '../lenses/tokens';
import { vesselPath } from '../vessel-shape';
import { ElementShell } from './common';
import { LENS_TEXT_MIN_PX } from './LensFill';

export type VesselProps = {
  item: VesselItem;
  token: HarborToken;
  dimmed: boolean;
  selected: boolean;
  /** Draw the name (≥ 9 px). */
  showName: boolean;
  pxPerCm: number;
  label: string;
  /** Length (cm) kept free from the stern (local x from 0) for the berth glyph drawn above; the name avoids it. */
  reserveCm?: number;
};

/**
 * A vessel silhouette in scale at its placement (m3-plan §8.2). Re-renders only when the allocation version,
 * the lens token or the level of detail change (React.memo on value props).
 */
export const Vessel = memo(function Vessel({
  item,
  token,
  dimmed,
  selected,
  showName,
  pxPerCm,
  label,
  reserveCm = 0,
}: VesselProps) {
  const d = vesselPath({ loa_cm: item.loa, beam_cm: item.beam, hull_type: item.hull });
  if (!d) return null;
  const tc = dimmed ? TOKEN_CLASSES.muted : TOKEN_CLASSES[token];
  const cls = tc.fill;
  const deg = item.rotationDd / 10;
  const reserve = Math.min(Math.max(0, reserveCm), item.loa * 0.5);
  const room = item.loa - reserve;
  // The name fits the free length of the hull (never under the glyph of the berth).
  const font = Math.min(item.beam * 0.4, (room * 0.9) / (Math.max(1, item.name.length) * 0.56));
  const nameX = reserve + room / 2;
  // The name has a halo in the hull colour (paint-order stroke) that keeps the deck lines out of the letters.
  // Keep the name readable: flip it when the bow points left.
  const flip = deg > 90 && deg < 270;
  const content = (
    <>
      <path
        d={d}
        className={`${cls} ${selected ? 'stroke-ring' : tc.onStroke}`}
        strokeWidth={selected ? 3 : 1}
        vectorEffect="non-scaling-stroke"
        fillRule="evenodd"
      />
      {showName && font * pxPerCm >= LENS_TEXT_MIN_PX ? (
        <text
          x={nameX}
          y={0}
          fontSize={font}
          textAnchor="middle"
          dominantBaseline="central"
          transform={flip ? `rotate(180 ${nameX} 0)` : undefined}
          className={`${tc.onFill} ${tc.stroke} font-semibold`}
          strokeWidth={font * 0.4}
          strokeLinejoin="round"
          paintOrder="stroke"
        >
          {item.name}
        </text>
      ) : null}
    </>
  );
  const transform = `translate(${item.origin.x} ${item.origin.y}) rotate(${deg})`;
  if (!item.interactive) {
    return (
      <g transform={transform} className="pointer-events-none" data-vessel={item.key} aria-hidden>
        {content}
      </g>
    );
  }
  return (
    <ElementShell
      refId={item.ref}
      kind="vessel"
      state={selected ? 'selected' : 'normal'}
      interactive
      label={label}
      transform={transform}
    >
      {content}
    </ElementShell>
  );
});
