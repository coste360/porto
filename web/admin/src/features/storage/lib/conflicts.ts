import { toProblem, type StorageScenarioConflict } from '@porto/shared/api';

/** The entries of a 409 scenario_conflicts apply error (m9-plan §7.3); empty for any other error. */
export function conflictsOf(error: unknown): StorageScenarioConflict[] {
  if (!error) return [];
  const p = toProblem(error);
  if (p.type !== 'scenario_conflicts') return [];
  const c = p.details.conflicts;
  return Array.isArray(c) ? (c as StorageScenarioConflict[]) : [];
}
