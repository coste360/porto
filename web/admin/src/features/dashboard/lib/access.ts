import { useCurrentMarina } from '../../../auth/hooks';

/** Whether the role may see any dashboard tile (spec 26.1: reports.operational or reports.all). */
export function useCanSeeDashboard(): boolean {
  const { can } = useCurrentMarina();
  return can('reports.all') || can('reports.operational');
}
