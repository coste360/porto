// Tabs of the phone screens, kept in the ?tab= search of their routes (m6b-plan §6.2, §6.3). The dock also
// accepts the extra tabs registered through DockSlots.extraTabs (M9 "Letture").
import { dockSlots } from '../slots';

export const DOCK_TABS = ['today', 'map', 'scan', 'cash', 'more'] as const;
export type DockTab = (typeof DOCK_TABS)[number];
export const DIRECTION_TABS = ['dashboard', 'map', 'deadlines'] as const;
export type DirectionTab = (typeof DIRECTION_TABS)[number];

export type DockSearch = { tab?: DockTab | (string & {}) };
export type DirectionSearch = { tab?: DirectionTab };

function pick<T extends string>(allowed: readonly T[], v: unknown): T | undefined {
  return typeof v === 'string' && (allowed as readonly string[]).includes(v) ? (v as T) : undefined;
}

/** Built-in dock tabs plus the registered extra tabs. */
export function dockTabValues(): string[] {
  return [...DOCK_TABS, ...(dockSlots().extraTabs ?? []).map((x) => x.value)];
}

export function validateDockSearch(search: Record<string, unknown>): DockSearch {
  const tab = pick(dockTabValues(), search.tab);
  return tab ? { tab } : {};
}

export function validateDirectionSearch(search: Record<string, unknown>): DirectionSearch {
  const tab = pick(DIRECTION_TABS, search.tab);
  return tab ? { tab } : {};
}
