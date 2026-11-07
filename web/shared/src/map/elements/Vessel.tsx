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
}: VesselProps) {
  const d = vesselPath({ loa_cm: item.loa, beam_cm: item.beam, hull_type: item.hull });
  if (!d) return null;
  const cls = dimmed ? TOKEN_CLASSES.muted.fill : TOKEN_CLASSES[token].fill;
  const deg = item.rotationDd / 10;
  const font = item.beam * 0.4;
  // Keep the name readable: flip it when the bow points left.
  const flip = deg > 90 && deg < 270;
  const content = (
    <>
      <path
        d={d}
        className={`${cls} ${selected ? 'stroke-ring' : 'stroke-foreground/70'}`}
        strokeWidth={selected ? 3 : 1}
        vectorEffect="non-scaling-stroke"
        fillRule="evenodd"
      />
      {showName && font * pxPerCm >= LENS_TEXT_MIN_PX ? (
        <text
          x={item.loa / 2}
          y={0}
          fontSize={font}
          textAnchor="middle"
          dominantBaseline="central"
          transform={flip ? `rotate(180 ${item.loa / 2} 0)` : undefined}
          className="fill-background font-semibold"
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
