import type { Schemas } from '@porto/shared/api';

// Enum values of the desk in display order; labels are occupancyEnums keys built from them.

/** Sources staff can record in M3 (customer_link and system come with M4/M12). */
export const STAFF_RELEASE_SOURCES = [
  'customer_phone',
  'customer_desk',
  'dock',
  'office',
] as const satisfies readonly Schemas['ReleaseSource'][];

export const INQUIRY_OUTCOMES = [
  'open',
  'quoted',
  'booked',
  'declined_no_space',
  'declined_size',
  'declined_other',
  'lost',
] as const satisfies readonly Schemas['InquiryOutcome'][];
