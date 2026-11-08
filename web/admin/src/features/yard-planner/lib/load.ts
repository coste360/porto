// Heatmap classes of the workload view (m10-plan §14.3): Tailwind theme tokens only (bg-chart-*, destructive).

/** Class of a load cell: empty, light, busy, full or overloaded. */
export function heatClass(
  loadBp: number | null | undefined,
  overload: boolean,
  assigned = 0,
): string {
  if (overload) return 'bg-destructive text-destructive-foreground';
  if (loadBp === null || loadBp === undefined)
    return assigned > 0 ? 'bg-chart-4 text-background' : 'bg-muted';
  if (loadBp === 0) return 'bg-muted';
  if (loadBp < 5000) return 'bg-chart-3 text-foreground';
  if (loadBp < 9000) return 'bg-chart-2 text-background';
  return 'bg-chart-5 text-foreground';
}

/** "75 %" from basis points (rounded down), "—" without capacity. */
export function loadLabel(loadBp: number | null | undefined): string {
  if (loadBp === null || loadBp === undefined) return '—';
  return `${Math.floor(loadBp / 100)} %`;
}
