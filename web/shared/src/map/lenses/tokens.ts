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
};

export const TOKEN_CLASSES: Readonly<Record<HarborToken, TokenClasses>> = {
  free: {
    fill: 'fill-harbor-free/25',
    stroke: 'stroke-harbor-free',
    bg: 'bg-harbor-free',
    text: 'text-harbor-free',
  },
  annual: {
    fill: 'fill-harbor-annual',
    stroke: 'stroke-harbor-annual',
    bg: 'bg-harbor-annual',
    text: 'text-harbor-annual',
  },
  seasonal: {
    fill: 'fill-harbor-seasonal',
    stroke: 'stroke-harbor-seasonal',
    bg: 'bg-harbor-seasonal',
    text: 'text-harbor-seasonal',
  },
  monthly: {
    fill: 'fill-harbor-monthly',
    stroke: 'stroke-harbor-monthly',
    bg: 'bg-harbor-monthly',
    text: 'text-harbor-monthly',
  },
  period: {
    fill: 'fill-harbor-period',
    stroke: 'stroke-harbor-period',
    bg: 'bg-harbor-period',
    text: 'text-harbor-period',
  },
  transit: {
    fill: 'fill-harbor-transit',
    stroke: 'stroke-harbor-transit',
    bg: 'bg-harbor-transit',
    text: 'text-harbor-transit',
  },
  hold: {
    fill: 'fill-harbor-hold',
    stroke: 'stroke-harbor-hold',
    bg: 'bg-harbor-hold',
    text: 'text-harbor-hold',
  },
  released: {
    fill: 'fill-harbor-released',
    stroke: 'stroke-harbor-released',
    bg: 'bg-harbor-released',
    text: 'text-harbor-released',
  },
  blocked: {
    fill: 'fill-harbor-blocked',
    stroke: 'stroke-harbor-blocked',
    bg: 'bg-harbor-blocked',
    text: 'text-harbor-blocked',
  },
  maintenance: {
    fill: 'fill-harbor-maintenance',
    stroke: 'stroke-harbor-maintenance',
    bg: 'bg-harbor-maintenance',
    text: 'text-harbor-maintenance',
  },
  owner: {
    fill: 'fill-harbor-owner',
    stroke: 'stroke-harbor-owner',
    bg: 'bg-harbor-owner',
    text: 'text-harbor-owner',
  },
  in: {
    fill: 'fill-harbor-in',
    stroke: 'stroke-harbor-in',
    bg: 'bg-harbor-in',
    text: 'text-harbor-in',
  },
  out: {
    fill: 'fill-harbor-out',
    stroke: 'stroke-harbor-out',
    bg: 'bg-harbor-out',
    text: 'text-harbor-out',
  },
  'away-long': {
    fill: 'fill-harbor-away-long',
    stroke: 'stroke-harbor-away-long',
    bg: 'bg-harbor-away-long',
    text: 'text-harbor-away-long',
  },
  arrival: {
    fill: 'fill-harbor-arrival',
    stroke: 'stroke-harbor-arrival',
    bg: 'bg-harbor-arrival',
    text: 'text-harbor-arrival',
  },
  departure: {
    fill: 'fill-harbor-departure',
    stroke: 'stroke-harbor-departure',
    bg: 'bg-harbor-departure',
    text: 'text-harbor-departure',
  },
  ok: {
    fill: 'fill-harbor-ok',
    stroke: 'stroke-harbor-ok',
    bg: 'bg-harbor-ok',
    text: 'text-harbor-ok',
  },
  warn: {
    fill: 'fill-harbor-warn',
    stroke: 'stroke-harbor-warn',
    bg: 'bg-harbor-warn',
    text: 'text-harbor-warn',
  },
  bad: {
    fill: 'fill-harbor-bad',
    stroke: 'stroke-harbor-bad',
    bg: 'bg-harbor-bad',
    text: 'text-harbor-bad',
  },
  override: {
    fill: 'fill-harbor-override',
    stroke: 'stroke-harbor-override',
    bg: 'bg-harbor-override',
    text: 'text-harbor-override',
  },
  muted: {
    fill: 'fill-harbor-muted/30',
    stroke: 'stroke-harbor-muted',
    bg: 'bg-harbor-muted',
    text: 'text-harbor-muted',
  },
};

export function tokenClasses(token: HarborToken): TokenClasses {
  return TOKEN_CLASSES[token];
}
