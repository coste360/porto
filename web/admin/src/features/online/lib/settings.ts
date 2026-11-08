import { z } from 'zod';

import type { OnlineBookingSettings, OnlineBookingSettingsInput } from '@porto/shared/api';

import { BOOKING_DOCUMENT_TYPES } from './constants';

const req = 'common:validation.required';
const time = z.string().regex(/^([01]\d|2[0-3]):[0-5]\d$/, req);
const int = (min: number, max: number) => z.number(req).int(req).min(min, req).max(max, req);

export const settingsSchema = z
  .object({
    rules: z.object({
      min_nights: int(1, 366),
      max_nights: int(1, 366),
      min_advance_hours: int(0, 8760),
      max_advance_days: int(1, 1095),
      arrival_from: time,
      arrival_to: time,
      closed_weekdays: z.array(z.number().int().min(0).max(6)).max(6),
    }),
    payment_mode: z.enum(['deposit', 'full']),
    deposit_bp: int(1, 10000),
    cancellation_tiers: z
      .array(z.object({ min_days_before: int(0, 1095), refund_bp: int(0, 10000) }))
      .min(1, req)
      .max(10)
      .refine((ts) => new Set(ts.map((x) => x.min_days_before)).size === ts.length, {
        message: 'online:settings.payment.tiers',
      }),
    refund_as: z.enum(['refund', 'credit']),
    approval: z.object({
      always: z.boolean(),
      loa_over_cm: z.number().int().min(1).max(10000).nullable(),
      group_ids: z.array(z.string()),
      hold_hours: int(1, 144),
    }),
    required_fields: z.object({
      documents: z.array(z.enum(BOOKING_DOCUMENT_TYPES as [string, ...string[]])),
      people_on_board: z.boolean(),
      phone: z.boolean(),
      documents_later: z.boolean(),
    }),
    reveal_berth: z.enum(['never', 'after_confirmation']),
    late_payment_recovery: z.boolean(),
    allow_without_provider: z.boolean(),
    max_live_holds: int(1, 10000),
    arrival_info: z.object({
      vhf_channel: z.string().max(64),
      phone: z.string().max(64),
      rules_url: z.union([z.literal(''), z.url().max(500)]),
      text_it: z.string().max(4000),
      text_en: z.string().max(4000),
    }),
    theme_preset: z.string().regex(/^[a-z0-9][a-z0-9-]{0,31}$/, req),
    hide_header_in_embed: z.boolean(),
    show_feet: z.boolean(),
  })
  .refine((v) => v.rules.min_nights <= v.rules.max_nights, {
    path: ['rules', 'max_nights'],
    message: req,
  });

export type SettingsValues = z.infer<typeof settingsSchema>;

export function toValues(s: OnlineBookingSettings): SettingsValues {
  return {
    rules: { ...s.rules, closed_weekdays: [...s.rules.closed_weekdays] },
    payment_mode: s.payment_mode,
    deposit_bp: s.deposit_bp,
    cancellation_tiers: s.cancellation_tiers.map((x) => ({ ...x })),
    refund_as: s.refund_as,
    approval: {
      always: s.approval.always,
      loa_over_cm: s.approval.loa_over_cm ?? null,
      group_ids: [...s.approval.group_ids],
      hold_hours: s.approval.hold_hours,
    },
    required_fields: { ...s.required_fields, documents: [...s.required_fields.documents] },
    reveal_berth: s.reveal_berth,
    late_payment_recovery: s.late_payment_recovery,
    allow_without_provider: s.allow_without_provider,
    max_live_holds: s.max_live_holds,
    arrival_info: {
      vhf_channel: s.arrival_info?.vhf_channel ?? '',
      phone: s.arrival_info?.phone ?? '',
      rules_url: s.arrival_info?.rules_url ?? '',
      text_it: s.arrival_info?.text?.it ?? '',
      text_en: s.arrival_info?.text?.en ?? '',
    },
    theme_preset: s.theme_preset,
    hide_header_in_embed: s.hide_header_in_embed,
    show_feet: s.show_feet,
  };
}

export function toInput(v: SettingsValues): OnlineBookingSettingsInput {
  const a = v.arrival_info;
  const text =
    a.text_it || a.text_en ? { it: a.text_it, ...(a.text_en ? { en: a.text_en } : {}) } : undefined;
  return {
    rules: v.rules,
    payment_mode: v.payment_mode,
    deposit_bp: v.deposit_bp,
    cancellation_tiers: v.cancellation_tiers,
    refund_as: v.refund_as,
    approval: {
      always: v.approval.always,
      group_ids: v.approval.group_ids,
      hold_hours: v.approval.hold_hours,
      ...(v.approval.loa_over_cm ? { loa_over_cm: v.approval.loa_over_cm } : {}),
    },
    required_fields: {
      ...v.required_fields,
      documents: v.required_fields
        .documents as OnlineBookingSettingsInput['required_fields']['documents'],
    },
    reveal_berth: v.reveal_berth,
    late_payment_recovery: v.late_payment_recovery,
    allow_without_provider: v.allow_without_provider,
    max_live_holds: v.max_live_holds,
    arrival_info: {
      ...(a.vhf_channel ? { vhf_channel: a.vhf_channel } : {}),
      ...(a.phone ? { phone: a.phone } : {}),
      ...(a.rules_url ? { rules_url: a.rules_url } : {}),
      ...(text ? { text } : {}),
    },
    theme_preset: v.theme_preset,
    hide_header_in_embed: v.hide_header_in_embed,
    show_feet: v.show_feet,
  };
}
