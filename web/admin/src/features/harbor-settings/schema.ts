import { z } from 'zod';

import type { BerthGroup, HarborSettings } from '@porto/shared/api';

const cmField = (min: number, max: number, message: string) =>
  z
    .number(message)
    .int()
    .min(min, message)
    .max(max, message)
    .nullable()
    .refine((v) => v !== null, message);

export const settingsSchema = z
  .object({
    loa_tolerance_cm: cmField(0, 500, 'maps:settings.validation.tolerance'),
    default_grid_cm: cmField(1, 10_000, 'maps:settings.validation.grid'),
    fallback: z.string().trim().max(16),
    bands: z.array(
      z.object({
        max_cm: cmField(1, 100_000, 'maps:settings.validation.bandMax'),
        class: z.string().trim().min(1, 'common:validation.required').max(16),
      }),
    ),
  })
  .superRefine((v, ctx) => {
    const seen = new Set<number>();
    v.bands.forEach((b, i) => {
      if (b.max_cm === null) return;
      if (seen.has(b.max_cm)) {
        ctx.addIssue({
          code: 'custom',
          path: ['bands', i, 'max_cm'],
          message: 'maps:settings.validation.bandDuplicate',
        });
      }
      seen.add(b.max_cm);
    });
  });

export type SettingsIn = z.input<typeof settingsSchema>;
export type SettingsOut = z.output<typeof settingsSchema>;

export function settingsDefaults(s: HarborSettings): SettingsIn {
  return {
    loa_tolerance_cm: s.loa_tolerance_cm,
    default_grid_cm: s.default_grid_cm,
    fallback: s.price_class_rule.fallback,
    bands: [...s.price_class_rule.bands]
      .sort((a, b) => a.max_cm - b.max_cm)
      .map((b) => ({ max_cm: b.max_cm, class: b.class })),
  };
}

/** PATCH body: bands sorted by width (the rule is "first band whose max ≥ width"). */
export function settingsPatch(v: SettingsOut) {
  return {
    loa_tolerance_cm: v.loa_tolerance_cm!,
    default_grid_cm: v.default_grid_cm!,
    price_class_rule: {
      basis: 'width' as const,
      fallback: v.fallback,
      bands: v.bands
        .map((b) => ({ max_cm: b.max_cm!, class: b.class }))
        .sort((a, b) => a.max_cm - b.max_cm),
    },
  };
}

export const GROUP_KINDS: readonly BerthGroup['kind'][] = [
  'structure',
  'transit',
  'area',
  'custom',
];

export const groupSchema = z.object({
  code: z.string().trim().min(1, 'common:validation.required').max(32),
  name_it: z.string().trim().min(1, 'common:validation.required').max(200),
  name_en: z.string().trim().max(200),
  kind: z.enum(GROUP_KINDS as [BerthGroup['kind'], ...BerthGroup['kind'][]]),
});

export type GroupForm = z.infer<typeof groupSchema>;
