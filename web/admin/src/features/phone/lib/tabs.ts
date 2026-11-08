// Tabs of the phone screens, kept in the ?tab= search of their routes (m6b-plan §6.2, §6.3).
export const DOCK_TABS = ['today', 'map', 'scan', 'cash', 'more'] as const;
export type DockTab = (typeof DOCK_TABS)[number];
export const DIRECTION_TABS = ['dashboard', 'map', 'deadlines'] as const;
export type DirectionTab = (typeof DIRECTION_TABS)[number];

export type DockSearch = { tab?: DockTab };
export type DirectionSearch = { tab?: DirectionTab };

function pick<T extends string>(allowed: readonly T[], v: unknown): T | undefined {
  return typeof v === 'string' && (allowed as readonly string[]).includes(v) ? (v as T) : undefined;
}

export function validateDockSearch(search: Record<string, unknown>): DockSearch {
  const tab = pick(DOCK_TABS, search.tab);
  return tab ? { tab } : {};
}

export function validateDirectionSearch(search: Record<string, unknown>): DirectionSearch {
  const tab = pick(DIRECTION_TABS, search.tab);
  return tab ? { tab } : {};
}
