import { useRentalUnits } from '@porto/shared/api';

import { useCurrentMarina } from '../../../auth/hooks';

/** The category of a unit (the checklist items of the settings may apply to some categories only). */
export function useUnitCategory(unitId: string): string {
  const { marinaId } = useCurrentMarina();
  const units = useRentalUnits(marinaId);
  return units.data?.items.find((u) => u.id === unitId)?.category ?? '';
}
