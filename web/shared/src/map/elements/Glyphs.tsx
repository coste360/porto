import { memo } from 'react';

import type { GlyphId } from '../lenses/types';

// Lens glyphs (spec 9.1: the state never depends on colour only). Simple shapes in a 100 × 100 box drawn once
// per map as <symbol>s and referenced with <use>.
const GLYPH_PATHS: Readonly<Record<GlyphId, string>> = {
  star: 'M50 6 L62 38 L96 38 L68 58 L79 92 L50 71 L21 92 L32 58 L4 38 L38 38 Z',
  suitcase: 'M36 18 H64 V30 H88 V86 H12 V30 H36 Z M44 26 V30 H56 V26 Z M12 50 H88 V58 H12 Z',
  clock: 'M50 6 A44 44 0 1 0 50.1 6 Z M46 22 V54 L70 68 L74 61 L54 50 V22 Z',
  release: 'M10 44 H62 V24 L94 50 L62 76 V56 H10 Z',
  wrench: 'M70 6 A26 26 0 0 0 46 40 L8 78 L22 92 L60 54 A26 26 0 0 0 94 30 L78 44 L60 40 L56 22 Z',
  lock: 'M24 44 V32 A26 26 0 0 1 76 32 V44 H86 V94 H14 V44 Z M36 44 H64 V32 A14 14 0 0 0 36 32 Z',
  arrowIn: 'M6 44 H58 V20 L94 50 L58 80 V56 H6 Z',
  arrowOut: 'M94 44 H42 V20 L6 50 L42 80 V56 H94 Z',
  calendar: 'M10 18 H90 V92 H10 Z M18 38 V84 H82 V38 Z M26 6 H36 V26 H26 Z M64 6 H74 V26 H64 Z',
  bolt: 'M58 4 L16 56 H44 L36 96 L84 40 H54 Z',
  drop: 'M50 4 C50 4 16 46 16 64 A34 34 0 0 0 84 64 C84 46 50 4 50 4 Z',
  alert: 'M50 4 L96 92 H4 Z M44 34 V64 H56 V34 Z M44 72 V84 H56 V72 Z',
  antenna:
    'M44 44 H56 V96 H44 Z M50 26 A12 12 0 1 0 50.1 26 Z M22 16 L30 24 A28 28 0 0 0 30 64 L22 72 A40 40 0 0 1 22 16 Z M78 16 L70 24 A28 28 0 0 1 70 64 L78 72 A40 40 0 0 0 78 16 Z',
};

export const GLYPH_IDS = Object.keys(GLYPH_PATHS) as GlyphId[];

export function glyphSymbolId(prefix: string, glyph: GlyphId): string {
  return `${prefix}-glyph-${glyph}`;
}

/** `<defs>` with the lens glyph symbols used on the map. */
export const GlyphDefs = memo(function GlyphDefs({
  prefix,
  glyphs,
}: {
  prefix: string;
  glyphs: ReadonlySet<GlyphId>;
}) {
  const used = [...glyphs].sort();
  if (used.length === 0) return null;
  return (
    <defs>
      {used.map((g) => (
        <symbol key={g} id={glyphSymbolId(prefix, g)} viewBox="0 0 100 100">
          <path d={GLYPH_PATHS[g]} fillRule="evenodd" />
        </symbol>
      ))}
    </defs>
  );
});

/** A standalone glyph (legend, quick card). */
export function GlyphIcon({ glyph, className }: { glyph: GlyphId; className?: string }) {
  return (
    <svg viewBox="0 0 100 100" className={className} aria-hidden="true" focusable="false">
      <path d={GLYPH_PATHS[glyph]} fillRule="evenodd" className="fill-current" />
    </svg>
  );
}
