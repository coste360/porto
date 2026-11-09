import { memo } from 'react';

import type { GlyphId } from '../lenses/types';
import { TOKEN_CLASSES, type HarborToken } from '../lenses/tokens';
import { glyphSymbolId } from './Glyphs';

/**
 * A point marker in map coordinates (M12, m12-plan §9.2): sensor devices on dynamic-area segments and pedestals.
 * Drawn at a constant on-screen size above the elements, with a lens glyph in a token colour and an optional "!"
 * badge (offline, silent, low battery).
 */
export type MapMarker = {
  key: string;
  /** Map coordinates (cm). */
  x: number;
  y: number;
  glyph: GlyphId;
  colorToken: HarborToken;
  badge?: boolean;
  /** Accessible name (also the tooltip). */
  label?: string;
  /** Element ref selected when the marker is activated (the device's resource). */
  ref?: string;
};

/** On-screen size of a marker glyph (px). */
export const MARKER_PX = 18;

export const MarkerLayer = memo(function MarkerLayer({
  markers,
  prefix,
  pxPerCm,
}: {
  markers: readonly MapMarker[];
  prefix: string;
  pxPerCm: number;
}) {
  if (markers.length === 0) return null;
  const size = MARKER_PX / pxPerCm;
  return (
    <g data-layer="markers">
      {markers.map((m) => (
        <g key={m.key} data-marker={m.key} data-ref={m.ref}>
          {m.label ? <title>{m.label}</title> : null}
          <use
            href={`#${glyphSymbolId(prefix, m.glyph)}`}
            x={m.x - size / 2}
            y={m.y - size / 2}
            width={size}
            height={size}
            className={`${TOKEN_CLASSES[m.colorToken].text} fill-current`}
          />
          {m.badge ? (
            <circle
              cx={m.x + size / 2}
              cy={m.y - size / 2}
              r={size / 4}
              className="fill-harbor-bad"
              data-testid="marker-badge"
            />
          ) : null}
        </g>
      ))}
    </g>
  );
});
