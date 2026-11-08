// Deadlines of the direction phone screen grouped by week (m6b-plan §6.3): Monday-based weeks, overdue items in
// the current week. Pure, on ISO dates.
const DAY = 86_400_000;

function utc(iso: string): number {
  return Date.parse(`${iso}T00:00:00Z`);
}

function iso(ms: number): string {
  return new Date(ms).toISOString().slice(0, 10);
}

/** Monday of the week of an ISO date. */
export function weekStart(isoDate: string): string {
  const ms = utc(isoDate);
  const dow = (new Date(ms).getUTCDay() + 6) % 7; // Monday = 0
  return iso(ms - dow * DAY);
}

export function groupByWeek<T extends { due_on: string }>(
  items: readonly T[],
  today: string,
): { weekStart: string; items: T[] }[] {
  const current = weekStart(today);
  const groups = new Map<string, T[]>();
  for (const it of [...items].sort((a, b) => a.due_on.localeCompare(b.due_on))) {
    const w = it.due_on < current ? current : weekStart(it.due_on);
    const list = groups.get(w) ?? [];
    list.push(it);
    groups.set(w, list);
  }
  return [...groups.entries()]
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([w, list]) => ({ weekStart: w, items: list }));
}
