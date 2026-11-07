import { z } from 'zod';

import type { BerthCategory, RevenueClass, Schemas } from '@porto/shared/api';

import { CATEGORY_TARGETS } from '../lib/enums';

// Forms of the classifications page: berth categories (codes of M1 `price_class`) and revenue classes.

const sortOrder = z
  .number('pricing:classifications.validation.sortOrder')
  .int('pricing:classifications.validation.sortOrder')
  .nullable()
  .transform((v) => v ?? 0);

export const categorySchema = z.object({
  code: z
    .string()
    .trim()
    .min(1, 'common:validation.required')
    .max(32, 'pricing:classifications.validation.categoryCode'),
  name_it: z.string().trim().min(1, 'common:validation.required').max(200),
  name_en: z.string().trim().max(200),
  applies_to: z.array(z.enum(CATEGORY_TARGETS)).min(1, 'common:validation.atLeastOne'),
  sort_order: sortOrder,
});
export type CategoryFormIn = z.input<typeof categorySchema>;
export type CategoryForm = z.output<typeof categorySchema>;

export function categoryDefaults(c: BerthCategory | null): CategoryFormIn {
  return c
    ? {
        code: c.code,
        name_it: c.name.it,
        name_en: c.name.en ?? '',
        applies_to: [...c.applies_to],
        sort_order: c.sort_order,
      }
    : { code: '', name_it: '', name_en: '', applies_to: ['berth'], sort_order: 0 };
}

const name = (it: string, en: string) => (en ? { it, en } : { it });

export function categoryCreateBody(v: CategoryForm): Schemas['BerthCategoryCreate'] {
  return {
    code: v.code,
    name: name(v.name_it, v.name_en),
    applies_to: v.applies_to,
    sort_order: v.sort_order,
  };
}

/** The code cannot change (rules and elements refer to it). */
export function categoryPatchBody(v: CategoryForm): Schemas['BerthCategoryPatch'] {
  return { name: name(v.name_it, v.name_en), applies_to: v.applies_to, sort_order: v.sort_order };
}

export const NO_PARENT = '__none__';

export const revenueClassSchema = z.object({
  code: z
    .string()
    .trim()
    .regex(/^[a-z0-9][a-z0-9_]{0,31}$/, 'pricing:classifications.validation.classCode'),
  name_it: z.string().trim().min(1, 'common:validation.required').max(200),
  name_en: z.string().trim().max(200),
  parent_id: z.string(),
  sort_order: sortOrder,
});
export type RevenueClassFormIn = z.input<typeof revenueClassSchema>;
export type RevenueClassForm = z.output<typeof revenueClassSchema>;

export function revenueClassDefaults(
  c: RevenueClass | null,
  parentId?: string,
): RevenueClassFormIn {
  return c
    ? {
        code: c.code,
        name_it: c.name.it,
        name_en: c.name.en ?? '',
        parent_id: c.parent_id ?? NO_PARENT,
        sort_order: c.sort_order,
      }
    : { code: '', name_it: '', name_en: '', parent_id: parentId ?? NO_PARENT, sort_order: 0 };
}

export function revenueClassCreateBody(v: RevenueClassForm): Schemas['RevenueClassCreate'] {
  return {
    code: v.code,
    name: name(v.name_it, v.name_en),
    ...(v.parent_id !== NO_PARENT ? { parent_id: v.parent_id } : {}),
    sort_order: v.sort_order,
  };
}

export function revenueClassPatchBody(v: RevenueClassForm): Schemas['RevenueClassPatch'] {
  return {
    code: v.code,
    name: name(v.name_it, v.name_en),
    parent_id: v.parent_id === NO_PARENT ? null : v.parent_id,
    sort_order: v.sort_order,
  };
}

/** Two-level tree: top-level classes by sort order, each followed by its children. */
export function revenueClassTree(classes: RevenueClass[]): { cls: RevenueClass; depth: 0 | 1 }[] {
  const bySort = (a: RevenueClass, b: RevenueClass) =>
    a.sort_order - b.sort_order || a.code.localeCompare(b.code);
  const ids = new Set(classes.map((c) => c.id));
  const roots = classes.filter((c) => !c.parent_id || !ids.has(c.parent_id)).sort(bySort);
  const out: { cls: RevenueClass; depth: 0 | 1 }[] = [];
  for (const r of roots) {
    out.push({ cls: r, depth: 0 });
    for (const c of classes.filter((x) => x.parent_id === r.id).sort(bySort)) {
      out.push({ cls: c, depth: 1 });
    }
  }
  return out;
}
