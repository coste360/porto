// Harbour lens colour tokens (--harbor-* in globals.css, m3-plan §8.2). Class names are written in full so the
// Tailwind scanner sees them; lenses only return token names, never colours.

export const HARBOR_TOKENS = [
  'free',
  'annual',
  'seasonal',
  'monthly',
  'period',
  'transit',
  'hold',
  'released',
  'blocked',
  'maintenance',
  'owner',
  'in',
  'out',
  'away-long',
  'arrival',
  'departure',
  'ok',
  'warn',
  'bad',
  'override',
  'muted',
  // M12 (m12-plan §9.2): a berth physically occupied according to its sensor (lens Sensori).
  'sensor-occupied',
] as const;

export type HarborToken = (typeof HARBOR_TOKENS)[number];

export type TokenClasses = {
  /** Fill of a berth, tile or silhouette. */
  fill: string;
  /** Outline (ghost, highlight). */
  stroke: string;
  /** Legend swatch and HTML badges. */
  bg: string;
  /** Glyphs and text drawn in the token colour. */
  text: string;
  /**
   * Text, glyphs and outlines drawn on top of the fill: the background colour on the solid fills (dark in the
   * light theme, light in the dark theme, so the opposite is always the background), the foreground on the
   * translucent free and muted fills.
   */
  onFill: string;
  onStroke: string;
};

export const TOKEN_CLASSES: Readonly<Record<HarborToken, TokenClasses>> = {
  free: {
    fill: 'fill-harbor-free/25',
    stroke: 'stroke-harbor-free',
    bg: 'bg-harbor-free',
    text: 'text-harbor-free',
    onFill: 'fill-foreground',
    onStroke: 'stroke-foreground/70',
  },
  annual: {
    fill: 'fill-harbor-annual',
    stroke: 'stroke-harbor-annual',
    bg: 'bg-harbor-annual',
    text: 'text-harbor-annual',
    onFill: 'fill-background',
    onStroke: 'stroke-background',
  },
  seasonal: {
    fill: 'fill-harbor-seasonal',
    stroke: 'stroke-harbor-seasonal',
    bg: 'bg-harbor-seasonal',
    text: 'text-harbor-seasonal',
    onFill: 'fill-background',
    onStroke: 'stroke-background',
  },
  monthly: {
    fill: 'fill-harbor-monthly',
    stroke: 'stroke-harbor-monthly',
    bg: 'bg-harbor-monthly',
    text: 'text-harbor-monthly',
    onFill: 'fill-background',
    onStroke: 'stroke-background',
  },
  period: {
    fill: 'fill-harbor-period',
    stroke: 'stroke-harbor-period',
    bg: 'bg-harbor-period',
    text: 'text-harbor-period',
    onFill: 'fill-background',
    onStroke: 'stroke-background',
  },
  transit: {
    fill: 'fill-harbor-transit',
    stroke: 'stroke-harbor-transit',
    bg: 'bg-harbor-transit',
    text: 'text-harbor-transit',
    onFill: 'fill-background',
    onStroke: 'stroke-background',
  },
  hold: {
    fill: 'fill-harbor-hold',
    stroke: 'stroke-harbor-hold',
    bg: 'bg-harbor-hold',
    text: 'text-harbor-hold',
    onFill: 'fill-background',
    onStroke: 'stroke-background',
  },
  released: {
    fill: 'fill-harbor-released',
    stroke: 'stroke-harbor-released',
    bg: 'bg-harbor-released',
    text: 'text-harbor-released',
    onFill: 'fill-background',
    onStroke: 'stroke-background',
  },
  blocked: {
    fill: 'fill-harbor-blocked',
    stroke: 'stroke-harbor-blocked',
    bg: 'bg-harbor-blocked',
    text: 'text-harbor-blocked',
    onFill: 'fill-background',
    onStroke: 'stroke-background',
  },
  maintenance: {
    fill: 'fill-harbor-maintenance',
    stroke: 'stroke-harbor-maintenance',
    bg: 'bg-harbor-maintenance',
    text: 'text-harbor-maintenance',
    onFill: 'fill-background',
    onStroke: 'stroke-background',
  },
  owner: {
    fill: 'fill-harbor-owner',
    stroke: 'stroke-harbor-owner',
    bg: 'bg-harbor-owner',
    text: 'text-harbor-owner',
    onFill: 'fill-background',
    onStroke: 'stroke-background',
  },
  in: {
    fill: 'fill-harbor-in',
    stroke: 'stroke-harbor-in',
    bg: 'bg-harbor-in',
    text: 'text-harbor-in',
    onFill: 'fill-background',
    onStroke: 'stroke-background',
  },
  out: {
    fill: 'fill-harbor-out',
    stroke: 'stroke-harbor-out',
    bg: 'bg-harbor-out',
    text: 'text-harbor-out',
    onFill: 'fill-background',
    onStroke: 'stroke-background',
  },
  'away-long': {
    fill: 'fill-harbor-away-long',
    stroke: 'stroke-harbor-away-long',
    bg: 'bg-harbor-away-long',
    text: 'text-harbor-away-long',
    onFill: 'fill-background',
    onStroke: 'stroke-background',
  },
  arrival: {
    fill: 'fill-harbor-arrival',
    stroke: 'stroke-harbor-arrival',
    bg: 'bg-harbor-arrival',
    text: 'text-harbor-arrival',
    onFill: 'fill-background',
    onStroke: 'stroke-background',
  },
  departure: {
    fill: 'fill-harbor-departure',
    stroke: 'stroke-harbor-departure',
    bg: 'bg-harbor-departure',
    text: 'text-harbor-departure',
    onFill: 'fill-background',
    onStroke: 'stroke-background',
  },
  ok: {
    fill: 'fill-harbor-ok',
    stroke: 'stroke-harbor-ok',
    bg: 'bg-harbor-ok',
    text: 'text-harbor-ok',
    onFill: 'fill-background',
    onStroke: 'stroke-background',
  },
  warn: {
    fill: 'fill-harbor-warn',
    stroke: 'stroke-harbor-warn',
    bg: 'bg-harbor-warn',
    text: 'text-harbor-warn',
    onFill: 'fill-background',
    onStroke: 'stroke-background',
  },
  bad: {
    fill: 'fill-harbor-bad',
    stroke: 'stroke-harbor-bad',
    bg: 'bg-harbor-bad',
    text: 'text-harbor-bad',
    onFill: 'fill-background',
    onStroke: 'stroke-background',
  },
  override: {
    fill: 'fill-harbor-override',
    stroke: 'stroke-harbor-override',
    bg: 'bg-harbor-override',
    text: 'text-harbor-override',
    onFill: 'fill-background',
    onStroke: 'stroke-background',
  },
  muted: {
    fill: 'fill-harbor-muted/30',
    stroke: 'stroke-harbor-muted',
    bg: 'bg-harbor-muted',
    text: 'text-harbor-muted',
    onFill: 'fill-foreground',
    onStroke: 'stroke-foreground/70',
  },
  'sensor-occupied': {
    fill: 'fill-harbor-sensor-occupied',
    stroke: 'stroke-harbor-sensor-occupied',
    bg: 'bg-harbor-sensor-occupied',
    text: 'text-harbor-sensor-occupied',
    onFill: 'fill-background',
    onStroke: 'stroke-background',
  },
};

export function tokenClasses(token: HarborToken): TokenClasses {
  return TOKEN_CLASSES[token];
}
