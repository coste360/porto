// Theme-token classes of the map renderer (m1-plan §5.2): Tailwind fill-*/stroke-* utilities on the theme
// tokens only, never hex values. Sizes and positions are SVG attributes.

export type ElementState = 'normal' | 'selected' | 'preview' | 'issue' | 'inactive';

export const MAP_TOKENS = {
  canvas: 'fill-background',
  mapFrame: 'fill-card stroke-border',
  water: 'fill-chart-2/15 stroke-chart-2/40',
  land: 'fill-muted stroke-border',
  concession: 'fill-none stroke-chart-4',
  grid: 'stroke-border/60',
  structure: {
    quay: 'fill-none stroke-foreground/70',
    pier: 'fill-secondary stroke-foreground/60',
    finger: 'fill-secondary stroke-foreground/60',
    breakwater: 'fill-muted-foreground/30 stroke-muted-foreground',
    channel: 'fill-chart-2/10 stroke-chart-2',
    walkway: 'fill-muted stroke-muted-foreground',
  },
  berth: 'fill-primary/15 stroke-primary/70',
  buoy: 'fill-primary/10 stroke-primary/60',
  area: 'fill-chart-3/20 stroke-chart-3',
  areaCell: 'fill-none stroke-chart-3/40',
  areaBlocked: 'fill-destructive/20 stroke-none',
  areaExit: 'fill-chart-5/40 stroke-none',
  land_resource: 'fill-chart-1/20 stroke-chart-1',
  pedestal: 'fill-chart-5 stroke-background',
  decoration: 'text-foreground fill-foreground',
  label: 'fill-foreground',
  labelMuted: 'fill-muted-foreground',
  /** Structure names: drawn over berths, with a halo in the background colour (paint-order stroke). */
  structureLabel: 'fill-foreground stroke-background font-semibold',
  /** Background of a structure name drawn over a line (structureLabelFit). */
  structureLabelPill: 'fill-background/90 stroke-border',
  scaleBar: 'fill-foreground stroke-foreground',
  measure: 'stroke-chart-4 fill-chart-4',
  guide: 'stroke-chart-4',
  marquee: 'fill-primary/10 stroke-primary',
  handle: 'fill-background stroke-primary',
  state: {
    normal: '',
    selected: 'stroke-ring fill-ring/30',
    preview: 'fill-chart-4/25 stroke-chart-4',
    issue: 'fill-destructive/25 stroke-destructive',
    inactive: 'opacity-50',
  },
} as const;

/** Class list of an element in a state (selected and issue override the base fill/stroke). */
export function elementClass(base: string, state: ElementState): string {
  if (state === 'normal') return base;
  if (state === 'inactive') return `${base} ${MAP_TOKENS.state.inactive}`;
  return MAP_TOKENS.state[state];
}
