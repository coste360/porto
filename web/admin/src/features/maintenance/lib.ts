// Pure helpers of the maintenance pages (m9-plan §12.2): element types, task ordering, periodicity text and the
// conflicts of a refused schedule.
import {
  isProblem,
  type MaintenanceConflict,
  type MaintenanceElementType,
  type MaintenancePlan,
  type MaintenanceTask,
} from '@porto/shared/api';

export const ELEMENT_TYPES: readonly MaintenanceElementType[] = [
  'berth',
  'structure',
  'dynamic_area',
  'land_resource',
  'pedestal',
  'equipment',
];

/** Overdue first, then by due date (no date last), then by title. */
export function sortTasks(list: readonly MaintenanceTask[]): MaintenanceTask[] {
  return [...list].sort((a, b) => {
    if (a.overdue !== b.overdue) return a.overdue ? -1 : 1;
    const da = a.due_on ?? '9999-12-31';
    const db = b.due_on ?? '9999-12-31';
    return da === db ? a.title.localeCompare(b.title) : da < db ? -1 : 1;
  });
}

type T = (key: string, params?: Record<string, unknown>) => string;

export function periodicityText(t: T, p: MaintenancePlan['periodicity']): string {
  switch (p.kind) {
    case 'months':
      return t('maintenance:period.months', { count: p.every ?? 0 });
    case 'fixed_date':
      return t('maintenance:period.fixedDate', { day: p.month_day ?? '' });
    default:
      return t('maintenance:period.cycles', { count: (p.every ?? 0) / 1000 });
  }
}

/** The conflicts of a 409 maintenance_conflicts, or null for another error. */
export function conflictsOf(error: unknown): MaintenanceConflict[] | null {
  if (!isProblem(error) || error.type !== 'maintenance_conflicts') return null;
  const list = (error.details as { conflicts?: unknown } | undefined)?.conflicts;
  return Array.isArray(list) ? (list as MaintenanceConflict[]) : [];
}
