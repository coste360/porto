import type { Schemas } from '@porto/shared/api';

// Enum values of the registry contract, in display order. Labels are i18n keys built from them.

export const CUSTOMER_KINDS = [
  'person',
  'company',
] as const satisfies readonly Schemas['CustomerKind'][];
export const CUSTOMER_CATEGORIES = [
  'member',
  'resident',
  'affiliated',
  'charter',
  'yard',
] as const satisfies readonly Schemas['CustomerCategory'][];
export const CONSENT_KINDS = [
  'privacy',
  'marketing',
  'third_party',
] as const satisfies readonly Schemas['ConsentKind'][];
export const CONSENT_SOURCES = [
  'desk',
  'online',
  'import',
  'api',
] as const satisfies readonly Schemas['ConsentSource'][];
export const HULL_TYPES = [
  'monohull_motor',
  'monohull_sail',
  'catamaran',
  'trimaran',
  'rib',
  'gozzo',
  'other',
] as const satisfies readonly Schemas['HullType'][];
export const FUELS = [
  'petrol',
  'diesel',
  'electric',
  'hybrid',
  'none',
  'other',
] as const satisfies readonly Schemas['Fuel'][];
export const POWER_RATINGS = [16, 32, 63, 125] as const;
export const PARTY_ROLES = [
  'owner',
  'operator',
  'skipper',
  'billing',
  'contact',
] as const satisfies readonly Schemas['VesselPartyRole'][];
export const DOCUMENT_TYPES = [
  'identity_card',
  'passport',
  'driving_licence',
  'boating_licence',
  'navigation_licence',
  'registration_certificate',
  'insurance_policy',
  'safety_certificate',
  'power_declaration',
  'signed_contract',
  'proxy',
  'photo',
  'other',
] as const satisfies readonly Schemas['DocumentType'][];
/** Document types offered for each owner (identity documents belong to people, D13). */
export const CUSTOMER_DOCUMENT_TYPES: readonly Schemas['DocumentType'][] = [
  'identity_card',
  'passport',
  'driving_licence',
  'boating_licence',
  'signed_contract',
  'proxy',
  'photo',
  'other',
];
export const VESSEL_DOCUMENT_TYPES: readonly Schemas['DocumentType'][] = [
  'navigation_licence',
  'registration_certificate',
  'insurance_policy',
  'safety_certificate',
  'power_declaration',
  'signed_contract',
  'photo',
  'other',
];
export const DEADLINE_STATUSES = [
  'open',
  'done',
  'dismissed',
] as const satisfies readonly Schemas['DeadlineStatus'][];
export const DEADLINE_URGENCIES = [
  'overdue',
  'due_soon',
  'later',
] as const satisfies readonly Schemas['DeadlineUrgency'][];
export const DEADLINE_KINDS = [
  'identity_document',
  'boating_licence',
  'navigation_licence',
  'registration_certificate',
  'insurance',
  'safety_certificate',
  'power_declaration',
  'other_document',
] as const satisfies readonly Schemas['DeadlineKind'][];
