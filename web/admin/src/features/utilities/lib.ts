// Pure helpers of the utilities pages (m9-plan §12.2).
import type { UtilityUnit } from '@porto/shared/api';

/** Unit of each meter kind (CHECK utility_meters_kind_unit_check). */
export const UNIT_OF: Record<string, UtilityUnit> = {
  power: 'kwh',
  water: 'm3',
  hours: 'h',
  cycles: 'cycle',
};
