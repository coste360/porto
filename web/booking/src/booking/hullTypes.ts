import type { HullType } from '@porto/shared/api';

/** Hull types offered by the public form, in display order (HullType of the API contract). */
export const HULL_TYPES: HullType[] = [
  'monohull_motor',
  'monohull_sail',
  'catamaran',
  'trimaran',
  'rib',
  'gozzo',
  'other',
];
