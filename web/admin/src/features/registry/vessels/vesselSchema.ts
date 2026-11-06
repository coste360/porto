import { z } from 'zod';

import type { Vessel, VesselInput, VesselPatch } from '@porto/shared/api';

import { FUELS, HULL_TYPES, POWER_RATINGS } from '../lib/enums';

const MAX_CM = 20_000 * 100; // 20 km: far beyond any vessel, keeps the values safe integers
const requiredLength = z
  .number('vessels:validation.positiveLength')
  .int()
  .positive('vessels:validation.positiveLength')
  .max(MAX_CM, 'vessels:validation.positiveLength');
const optionalLength = z
  .number()
  .int()
  .positive('vessels:validation.positiveLength')
  .max(MAX_CM, 'vessels:validation.positiveLength')
  .nullable();
const optionalInt = z
  .string()
  .trim()
  .regex(/^\d{0,9}$/, 'vessels:validation.integer');

const engine = z.object({
  make: z.string().trim().max(100),
  model: z.string().trim().max(100),
  power_kw: optionalInt,
  power_hp: optionalInt,
  serial: z.string().trim().max(100),
  hours: optionalInt,
});

export const vesselSchema = z.object({
  name: z.string().trim().min(1, 'common:validation.required').max(100),
  registration: z.string().trim().max(50),
  registry_office: z.string().trim().max(100),
  flag: z.string().trim().max(2),
  home_port: z.string().trim().max(100),
  hull_type: z.enum(HULL_TYPES),
  model: z.string().trim().max(100),
  builder: z.string().trim().max(100),
  year_built: z
    .string()
    .trim()
    .regex(/^(|1[89]\d\d|2\d\d\d)$/, 'vessels:validation.year'),
  hull_color: z.string().trim().max(50),
  hull_material: z.string().trim().max(50),
  loa_cm: requiredLength
    .nullable()
    .refine((v): boolean => v !== null, 'vessels:validation.positiveLength'),
  lwl_cm: optionalLength,
  beam_cm: requiredLength
    .nullable()
    .refine((v): boolean => v !== null, 'vessels:validation.positiveLength'),
  draft_cm: optionalLength,
  air_draft_cm: optionalLength,
  displacement_kg: optionalInt,
  engines: z.array(engine).max(8),
  fuel: z.union([z.enum(FUELS), z.literal('')]),
  power_required_a: z.union([
    z.enum(POWER_RATINGS.map(String) as [string, ...string[]]),
    z.literal(''),
  ]),
  dimensions_source: z.enum(['declared', 'measured']),
  notes: z.string().max(4000),
});

export type VesselFormValues = z.infer<typeof vesselSchema>;

export function emptyVesselValues(): VesselFormValues {
  return {
    name: '',
    registration: '',
    registry_office: '',
    flag: 'IT',
    home_port: '',
    hull_type: 'monohull_motor',
    model: '',
    builder: '',
    year_built: '',
    hull_color: '',
    hull_material: '',
    loa_cm: null,
    lwl_cm: null,
    beam_cm: null,
    draft_cm: null,
    air_draft_cm: null,
    displacement_kg: '',
    engines: [],
    fuel: '',
    power_required_a: '',
    dimensions_source: 'declared',
    notes: '',
  };
}

const str = (n: number | null | undefined) => (n === null || n === undefined ? '' : String(n));

export function vesselToValues(v: Vessel): VesselFormValues {
  return {
    name: v.name,
    registration: v.registration ?? '',
    registry_office: v.registry_office ?? '',
    flag: v.flag ?? '',
    home_port: v.home_port ?? '',
    hull_type: v.hull_type,
    model: v.model ?? '',
    builder: v.builder ?? '',
    year_built: str(v.year_built),
    hull_color: v.hull_color ?? '',
    hull_material: v.hull_material ?? '',
    loa_cm: v.loa_cm,
    lwl_cm: v.lwl_cm,
    beam_cm: v.beam_cm,
    draft_cm: v.draft_cm,
    air_draft_cm: v.air_draft_cm,
    displacement_kg: str(v.displacement_kg),
    engines: v.engines.map((e) => ({
      make: e.make ?? '',
      model: e.model ?? '',
      power_kw: str(e.power_kw),
      power_hp: str(e.power_hp),
      serial: e.serial ?? '',
      hours: str(e.hours),
    })),
    fuel: v.fuel ?? '',
    power_required_a: str(v.power_required_a),
    dimensions_source: v.dimensions_source,
    notes: v.notes,
  };
}

function engines(v: VesselFormValues): VesselInput['engines'] {
  return v.engines
    .map((e) => ({
      ...(e.make ? { make: e.make } : {}),
      ...(e.model ? { model: e.model } : {}),
      ...(e.power_kw ? { power_kw: Number(e.power_kw) } : {}),
      ...(e.power_hp ? { power_hp: Number(e.power_hp) } : {}),
      ...(e.serial ? { serial: e.serial } : {}),
      ...(e.hours ? { hours: Number(e.hours) } : {}),
    }))
    .filter((e) => Object.keys(e).length > 0);
}

export function valuesToVesselInput(
  v: VesselFormValues,
  parties: VesselInput['parties'] = [],
): VesselInput {
  return {
    name: v.name,
    hull_type: v.hull_type,
    loa_cm: v.loa_cm!,
    beam_cm: v.beam_cm!,
    ...(v.registration ? { registration: v.registration } : {}),
    ...(v.registry_office ? { registry_office: v.registry_office } : {}),
    ...(v.flag ? { flag: v.flag.toUpperCase() } : {}),
    ...(v.home_port ? { home_port: v.home_port } : {}),
    ...(v.model ? { model: v.model } : {}),
    ...(v.builder ? { builder: v.builder } : {}),
    ...(v.year_built ? { year_built: Number(v.year_built) } : {}),
    ...(v.hull_color ? { hull_color: v.hull_color } : {}),
    ...(v.hull_material ? { hull_material: v.hull_material } : {}),
    ...(v.lwl_cm !== null ? { lwl_cm: v.lwl_cm } : {}),
    ...(v.draft_cm !== null ? { draft_cm: v.draft_cm } : {}),
    ...(v.air_draft_cm !== null ? { air_draft_cm: v.air_draft_cm } : {}),
    ...(v.displacement_kg ? { displacement_kg: Number(v.displacement_kg) } : {}),
    engines: engines(v),
    ...(v.fuel ? { fuel: v.fuel } : {}),
    ...(v.power_required_a
      ? { power_required_a: Number(v.power_required_a) as 16 | 32 | 63 | 125 }
      : {}),
    ...(v.notes ? { notes: v.notes } : {}),
    ...(parties && parties.length > 0 ? { parties } : {}),
  };
}

export function valuesToVesselPatch(v: VesselFormValues): VesselPatch {
  return {
    name: v.name,
    registration: v.registration || null,
    registry_office: v.registry_office || null,
    flag: v.flag ? v.flag.toUpperCase() : null,
    home_port: v.home_port || null,
    hull_type: v.hull_type,
    model: v.model || null,
    builder: v.builder || null,
    year_built: v.year_built ? Number(v.year_built) : null,
    hull_color: v.hull_color || null,
    hull_material: v.hull_material || null,
    loa_cm: v.loa_cm!,
    lwl_cm: v.lwl_cm,
    beam_cm: v.beam_cm!,
    draft_cm: v.draft_cm,
    air_draft_cm: v.air_draft_cm,
    displacement_kg: v.displacement_kg ? Number(v.displacement_kg) : null,
    dimensions_source: v.dimensions_source,
    engines: engines(v),
    fuel: v.fuel || null,
    power_required_a: v.power_required_a ? Number(v.power_required_a) : null,
    notes: v.notes,
  };
}
