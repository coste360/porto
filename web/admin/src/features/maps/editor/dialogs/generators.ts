// Generator forms (spec 8.3, m1-plan §2.5): field specs per generator, defaults, and the GeneratorParams body.
import type { GeneratorParams } from '@porto/shared/api';
import { MIN_RECT_SIDE_CM, type Pt } from '@porto/shared/map';

export type GeneratorType = GeneratorParams['type'];

export type FieldKind =
  | 'cm'
  | 'int'
  | 'angle'
  | 'point'
  | 'polygon'
  | 'text'
  | 'bool'
  | 'sides'
  | 'numbering'
  | 'structure'
  | 'group'
  | 'select';

export type FieldSpec = {
  name: string;
  kind: FieldKind;
  required?: boolean;
  /** select: the option values (labels from editor:options.<value>). */
  options?: readonly string[];
  /** structure: which structure kinds can be picked. */
  structureKinds?: readonly string[];
  /** cm fields: 0 allowed. */
  allowZero?: boolean;
  /** cm fields: smallest non-zero value (berth and land resource sides: MIN_RECT_SIDE_CM). */
  min?: number;
};

export type Numbering = {
  scheme: 'continuous' | 'odd_left_even_right' | 'per_side';
  prefix: string;
  start: number;
  pad: number;
};
export type GeneratorValues = Record<string, unknown>;

const MOORING = ['mooring_line', 'finger', 'alongside', 'piles'] as const;
const SIDE_LR = ['left', 'right'] as const;

export const GENERATOR_FIELDS: Record<GeneratorType, readonly FieldSpec[]> = {
  pier: [
    { name: 'code', kind: 'text', required: true },
    { name: 'origin', kind: 'point', required: true },
    { name: 'rotation_dd', kind: 'angle', required: true },
    { name: 'root_id', kind: 'structure', structureKinds: ['quay'] },
    { name: 'length_cm', kind: 'cm', required: true },
    { name: 'width_cm', kind: 'cm', required: true },
    { name: 'floating', kind: 'bool' },
    { name: 'sides', kind: 'sides', required: true },
    { name: 'berths_per_side', kind: 'int' },
    { name: 'start_offset_cm', kind: 'cm', allowZero: true },
    { name: 'berth_width_cm', kind: 'cm', min: MIN_RECT_SIDE_CM, required: true },
    { name: 'berth_length_cm', kind: 'cm', min: MIN_RECT_SIDE_CM, required: true },
    { name: 'berth_width_end_cm', kind: 'cm', min: MIN_RECT_SIDE_CM, allowZero: true },
    { name: 'berth_length_end_cm', kind: 'cm', min: MIN_RECT_SIDE_CM, allowZero: true },
    { name: 'finger_every', kind: 'int' },
    { name: 'finger_width_cm', kind: 'cm', allowZero: true },
    { name: 'finger_length_cm', kind: 'cm', allowZero: true },
    { name: 'head_berths', kind: 'int' },
    { name: 'mooring_type', kind: 'select', options: MOORING },
    { name: 'numbering', kind: 'numbering', required: true },
    { name: 'depth_cm', kind: 'cm', allowZero: true },
    { name: 'group_id', kind: 'group' },
  ],
  quay_berths: [
    { name: 'quay_id', kind: 'structure', required: true, structureKinds: ['quay'] },
    { name: 'from_cm', kind: 'cm', required: true, allowZero: true },
    { name: 'to_cm', kind: 'cm', required: true },
    { name: 'side', kind: 'select', required: true, options: SIDE_LR },
    { name: 'berth_width_cm', kind: 'cm', min: MIN_RECT_SIDE_CM, required: true },
    { name: 'count', kind: 'int' },
    { name: 'berth_length_cm', kind: 'cm', min: MIN_RECT_SIDE_CM, required: true },
    { name: 'mooring_type', kind: 'select', options: MOORING },
    { name: 'numbering', kind: 'numbering', required: true },
    { name: 'group_id', kind: 'group' },
  ],
  linear_area: [
    { name: 'code', kind: 'text', required: true },
    {
      name: 'structure_id',
      kind: 'structure',
      required: true,
      structureKinds: ['pier', 'quay', 'walkway'],
    },
    { name: 'side', kind: 'select', required: true, options: ['left', 'right', 'head'] },
    { name: 'from_cm', kind: 'cm', required: true, allowZero: true },
    { name: 'to_cm', kind: 'cm', required: true },
    { name: 'mode', kind: 'select', required: true, options: ['stern_to', 'alongside'] },
    { name: 'cell_cm', kind: 'cm' },
    { name: 'margin_cm', kind: 'cm', allowZero: true },
    { name: 'max_loa_cm', kind: 'cm' },
    { name: 'max_beam_cm', kind: 'cm' },
    { name: 'group_id', kind: 'group' },
  ],
  buoy_field: [
    { name: 'outline', kind: 'polygon', required: true },
    { name: 'swing_radius_cm', kind: 'cm', required: true },
    { name: 'layout', kind: 'select', required: true, options: ['grid', 'staggered'] },
    { name: 'spacing_cm', kind: 'cm' },
    { name: 'max_loa_cm', kind: 'cm' },
    { name: 'numbering', kind: 'numbering', required: true },
    { name: 'group_id', kind: 'group' },
  ],
  land_grid: [
    { name: 'origin', kind: 'point', required: true },
    { name: 'rotation_dd', kind: 'angle', required: true },
    { name: 'rows', kind: 'int', required: true },
    { name: 'cols', kind: 'int', required: true },
    { name: 'item_width_cm', kind: 'cm', min: MIN_RECT_SIDE_CM, required: true },
    { name: 'item_length_cm', kind: 'cm', min: MIN_RECT_SIDE_CM, required: true },
    { name: 'aisle_cm', kind: 'cm', allowZero: true },
    { name: 'aisle_every', kind: 'int' },
    {
      name: 'kind',
      kind: 'select',
      required: true,
      options: ['parking', 'box', 'storeroom', 'locker', 'cradle', 'trailer_spot', 'custom'],
    },
    { name: 'numbering', kind: 'numbering', required: true },
    { name: 'group_id', kind: 'group' },
  ],
  surface_area: [
    { name: 'code', kind: 'text', required: true },
    { name: 'outline', kind: 'polygon', required: true },
    { name: 'cell_cm', kind: 'cm', required: true },
    { name: 'grid_rotation_dd', kind: 'select', options: ['0', '900', '1800', '2700'] },
    { name: 'margin_cm', kind: 'cm', allowZero: true },
    { name: 'max_loa_cm', kind: 'cm' },
    { name: 'max_height_cm', kind: 'cm' },
    { name: 'group_id', kind: 'group' },
  ],
};

const numbering = (scheme: Numbering['scheme'], prefix: string): Numbering => ({
  scheme,
  prefix,
  start: 1,
  pad: 0,
});

export function generatorDefaults(type: GeneratorType): GeneratorValues {
  switch (type) {
    case 'pier':
      return {
        code: 'A',
        origin: null,
        rotation_dd: 2700,
        root_id: '',
        length_cm: 10_000,
        width_cm: 300,
        floating: false,
        sides: ['left', 'right'],
        berths_per_side: 0,
        start_offset_cm: 0,
        berth_width_cm: 400,
        berth_length_cm: 1200,
        berth_width_end_cm: null,
        berth_length_end_cm: null,
        finger_every: 0,
        finger_width_cm: null,
        finger_length_cm: null,
        head_berths: 0,
        mooring_type: 'mooring_line',
        numbering: numbering('odd_left_even_right', 'A-'),
        depth_cm: null,
        group_id: '',
      };
    case 'quay_berths':
      return {
        quay_id: '',
        from_cm: 0,
        to_cm: 5000,
        side: 'left',
        berth_width_cm: 400,
        count: 0,
        berth_length_cm: 1200,
        mooring_type: 'mooring_line',
        numbering: numbering('continuous', 'Q-'),
        group_id: '',
      };
    case 'linear_area':
      return {
        code: 'T',
        structure_id: '',
        side: 'left',
        from_cm: 0,
        to_cm: 5000,
        mode: 'stern_to',
        cell_cm: 50,
        margin_cm: 50,
        max_loa_cm: null,
        max_beam_cm: 1200,
        group_id: '',
      };
    case 'buoy_field':
      return {
        outline: [],
        swing_radius_cm: 1400,
        layout: 'staggered',
        spacing_cm: null,
        max_loa_cm: null,
        numbering: numbering('continuous', 'BOA-'),
        group_id: '',
      };
    case 'land_grid':
      return {
        origin: null,
        rotation_dd: 0,
        rows: 6,
        cols: 10,
        item_width_cm: 250,
        item_length_cm: 500,
        aisle_cm: 600,
        aisle_every: 2,
        kind: 'parking',
        numbering: numbering('continuous', 'P'),
        group_id: '',
      };
    case 'surface_area':
      return {
        code: 'PZ-1',
        outline: [],
        cell_cm: 100,
        grid_rotation_dd: '0',
        margin_cm: 60,
        max_loa_cm: null,
        max_height_cm: null,
        group_id: '',
      };
  }
}

function isEmpty(v: unknown): boolean {
  return v === null || v === undefined || v === '' || (Array.isArray(v) && v.length === 0);
}

/**
 * The request body of a generator, or the names of the fields that are missing or invalid. Optional empty
 * fields are left out so the server applies its defaults.
 */
export function buildGenerator(
  type: GeneratorType,
  values: GeneratorValues,
  keyPrefix: string,
): { params: GeneratorParams } | { errors: string[] } {
  const errors: string[] = [];
  const out: Record<string, unknown> = { type, key_prefix: keyPrefix };
  for (const f of GENERATOR_FIELDS[type]) {
    const v = values[f.name];
    if (isEmpty(v)) {
      if (f.required) errors.push(f.name);
      continue;
    }
    switch (f.kind) {
      case 'cm':
      case 'int':
      case 'angle':
        if (
          typeof v !== 'number' ||
          !Number.isSafeInteger(v) ||
          v < 0 ||
          (f.kind === 'cm' && !f.allowZero && v === 0) ||
          (f.kind === 'cm' && f.min !== undefined && v !== 0 && v < f.min)
        ) {
          errors.push(f.name);
          continue;
        }
        out[f.name] = v;
        break;
      case 'polygon':
        if (!Array.isArray(v) || v.length < 3) {
          errors.push(f.name);
          continue;
        }
        out[f.name] = (v as Pt[]).map((p) => ({ x: p.x, y: p.y }));
        break;
      case 'select':
        out[f.name] = f.name === 'grid_rotation_dd' ? Number(v) : v;
        break;
      default:
        out[f.name] = v;
    }
  }
  if (type === 'pier' || type === 'quay_berths' || type === 'linear_area') {
    const from = values.from_cm as number | null;
    const to = values.to_cm as number | null;
    if (typeof from === 'number' && typeof to === 'number' && from >= to) errors.push('to_cm');
  }
  return errors.length > 0 ? { errors } : { params: out as GeneratorParams };
}
