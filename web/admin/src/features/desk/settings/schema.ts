import { z } from 'zod';

import type { OccupancySettings, Schemas } from '@porto/shared/api';

// Occupancy settings form (m3-plan §5.1 ranges). Integers only; messages are i18n keys.

const int = (min: number, max: number) =>
  z
    .number('desk:settings.validation.range')
    .int('desk:settings.validation.range')
    .min(min, 'desk:settings.validation.range')
    .max(max, 'desk:settings.validation.range')
    .nullable()
    .refine((v) => v !== null, 'desk:settings.validation.range');

const time = z.string().regex(/^([01][0-9]|2[0-3]):[0-5][0-9]$/, 'desk:settings.validation.time');

export const occupancySettingsSchema = z.object({
  safety_clearance_cm: int(0, 300),
  waste_threshold_bp: int(0, 10000),
  draft_hold_days: int(1, 60),
  desk_hold_hours: int(1, 168),
  quote_validity_days: int(1, 120),
  max_segments: int(0, 2),
  absent_days_threshold: int(1, 365),
  arrival_grace_minutes: int(0, 1440),
  default_arrival_time: time,
  default_departure_time: time,
  reprice_on_move: z.boolean(),
  release_policy: z.enum(['never', 'with_consent']),
});

export type OccupancySettingsIn = z.input<typeof occupancySettingsSchema>;
export type OccupancySettingsOut = z.output<typeof occupancySettingsSchema>;

export function settingsDefaults(s: OccupancySettings): OccupancySettingsIn {
  const { version: _v, ...rest } = s;
  return rest;
}

export function settingsBody(v: OccupancySettingsOut): Schemas['OccupancySettingsUpdate'] {
  return {
    safety_clearance_cm: v.safety_clearance_cm!,
    waste_threshold_bp: v.waste_threshold_bp!,
    draft_hold_days: v.draft_hold_days!,
    desk_hold_hours: v.desk_hold_hours!,
    quote_validity_days: v.quote_validity_days!,
    max_segments: v.max_segments!,
    absent_days_threshold: v.absent_days_threshold!,
    arrival_grace_minutes: v.arrival_grace_minutes!,
    default_arrival_time: v.default_arrival_time,
    default_departure_time: v.default_departure_time,
    reprice_on_move: v.reprice_on_move,
    release_policy: v.release_policy,
  };
}

/** Integer fields of the form in display order, with the input kind. */
export const SETTINGS_FIELDS = [
  { name: 'safety_clearance_cm', kind: 'length' },
  { name: 'waste_threshold_bp', kind: 'percent' },
  { name: 'draft_hold_days', kind: 'integer' },
  { name: 'desk_hold_hours', kind: 'integer' },
  { name: 'quote_validity_days', kind: 'integer' },
  { name: 'max_segments', kind: 'integer' },
  { name: 'absent_days_threshold', kind: 'integer' },
  { name: 'arrival_grace_minutes', kind: 'integer' },
] as const;
