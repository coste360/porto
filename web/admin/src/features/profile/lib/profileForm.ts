import { z } from 'zod';

import type {
  MarinaProfile,
  MarinaProfileInput,
  ProfileHours,
  ProfilePhoto,
  ProfilePoi,
  ProfileServices,
  ProfileText,
  ProfileWind,
} from '@porto/shared/api';
import { formatE7, parseDecimalToE7 } from '@porto/shared/lib/format';

// Form model of the public profile panel (m12-plan §16.2): texts per language, positions typed in decimal degrees
// and kept as e7 integers, depths and lengths kept as integer cm. No float is ever stored.

const req = 'common:validation.required';
const url = 'common:validation.url';
const email = 'common:validation.email';

export const WINDS: ProfileWind[] = ['N', 'NE', 'E', 'SE', 'S', 'SW', 'W', 'NW'];
export const SEABEDS = ['sand', 'mud', 'rock', 'weed', 'mixed', 'other'] as const;
export const POWER_AMPS = [16, 32, 63, 125] as const;
export const SERVICE_FLAGS = [
  'water',
  'wifi',
  'toilets',
  'showers',
  'laundry',
  'pump_out',
  'waste',
  'slipway',
  'yard',
  'storage',
  'rental',
  'parking',
  'restaurant',
  'shops',
  'transport',
] as const;
export type ServiceFlag = (typeof SERVICE_FLAGS)[number];
export const POI_KINDS = ['entrance', 'fuel', 'office', 'pump_out'] as const;

/** Latitude/longitude limits in degrees × 10^7. */
const LAT_MAX = 900_000_000;
const LON_MAX = 1_800_000_000;

/**
 * Parses decimal degrees typed by the user ("43.7101234", "10,4") into degrees × 10^7 with exact digit
 * arithmetic, null for an invalid text or a value outside the range of the axis.
 */
export function parseDegrees(text: string, axis: 'lat' | 'lon'): number | null {
  const v = parseDecimalToE7(text);
  if (v === null) return null;
  const limit = axis === 'lat' ? LAT_MAX : LON_MAX;
  return Math.abs(v) <= limit ? v : null;
}

/** Degrees × 10^7 as the text of the field ("" when empty). */
export function degreesText(e7: number | null | undefined): string {
  return e7 === null || e7 === undefined ? '' : formatE7(e7);
}

const text = (max: number) =>
  z.object({ it: z.string().max(max, req), en: z.string().max(max, req) });
const hours = z.array(
  z
    .object({
      weekday: z.number().int().min(1).max(7),
      from: z.string().regex(/^([01]\d|2[0-3]):[0-5]\d$/, req),
      to: z.string().regex(/^(([01]\d|2[0-3]):[0-5]\d|24:00)$/, req),
    })
    .refine((h) => h.to > h.from, { path: ['to'], message: req }),
);
const cm = z.number().int().min(0).nullable();
const photo = z.object({ file_id: z.string(), alt: text(200) });

export const profileSchema = z
  .object({
    locales: z.array(z.enum(['it', 'en'])),
    lat: z.string().refine((s) => s.trim() === '' || parseDegrees(s, 'lat') !== null, req),
    lon: z.string().refine((s) => s.trim() === '' || parseDegrees(s, 'lon') !== null, req),
    street: z.string().max(200),
    postal_code: z.string().max(16),
    city: z.string().max(100),
    province: z.string().max(8),
    country: z.union([z.literal(''), z.string().regex(/^[A-Za-z]{2}$/, req)]),
    phone: z.string().max(40),
    email: z.union([z.literal(''), z.email(email).max(254)]),
    website: z.union([z.literal(''), z.url({ protocol: /^https$/, message: url }).max(300)]),
    vhf: z.array(z.number().int().min(1).max(88)).max(4),
    office_hours: hours.max(21),
    dock_hours: hours.max(21),
    hours_note: text(300),
    berths_total_override: z.number().int().min(0).nullable(),
    transit_berths_override: z.number().int().min(0).nullable(),
    max_loa_cm: cm,
    depth_entrance_cm: cm,
    depth_quay_cm: cm,
    depth_min_cm: cm,
    depth_max_cm: cm,
    seabed: z.union([z.literal(''), z.enum(SEABEDS)]),
    shelter: z.array(z.enum(['N', 'NE', 'E', 'SE', 'S', 'SW', 'W', 'NW'])),
    access_notes: text(2000),
    lights: text(1000),
    description: text(1000),
    services: z.object({
      flags: z.array(z.enum(SERVICE_FLAGS)),
      power_amps: z.array(z.number().int()),
      petrol: z.boolean(),
      diesel: z.boolean(),
      fuel_hours: text(300),
      travel_lift_kg: z.number().int().min(0).nullable(),
      crane_kg: z.number().int().min(0).nullable(),
    }),
    photos: z.array(photo).max(12),
    access_images: z.array(photo).max(4),
    show_map: z.boolean(),
    map_id: z.string(),
    show_prices: z.boolean(),
    show_occupancy: z.boolean(),
    occupancy_categories: z.array(z.string()).max(20),
    poi: z.array(
      z.object({ kind: z.enum(POI_KINDS), x_cm: z.number().int(), y_cm: z.number().int() }),
    ),
  })
  .refine((v) => (v.lat.trim() === '') === (v.lon.trim() === ''), { path: ['lon'], message: req })
  .refine(
    (v) => v.depth_min_cm === null || v.depth_max_cm === null || v.depth_min_cm <= v.depth_max_cm,
    { path: ['depth_max_cm'], message: req },
  )
  .refine((v) => !v.show_occupancy || v.occupancy_categories.length > 0, {
    path: ['occupancy_categories'],
    message: 'common:validation.atLeastOne',
  });

export type ProfileValues = z.infer<typeof profileSchema>;
export type ProfileTextValue = { it: string; en: string };

function textOf(t: ProfileText | undefined): ProfileTextValue {
  return { it: t?.it ?? '', en: t?.en ?? '' };
}

function textIn(t: ProfileTextValue): ProfileText {
  const out: ProfileText = {};
  if (t.it.trim()) out.it = t.it.trim();
  if (t.en.trim()) out.en = t.en.trim();
  return out;
}

function photosOf(ps: ProfilePhoto[] | undefined): ProfileValues['photos'] {
  return [...(ps ?? [])]
    .sort((a, b) => (a.sort ?? 0) - (b.sort ?? 0))
    .map((p) => ({ file_id: p.file_id, alt: textOf(p.alt) }));
}

function hoursOf(hs: ProfileHours[] | undefined): ProfileValues['office_hours'] {
  return (hs ?? []).map((h) => ({ weekday: h.weekday, from: h.from, to: h.to }));
}

/** The form values of a profile (draft or defaults). */
export function toValues(p: MarinaProfile): ProfileValues {
  const s: ProfileServices = p.services ?? {};
  return {
    locales: (p.locales ?? ['it']).filter((l): l is 'it' | 'en' => l === 'it' || l === 'en'),
    lat: degreesText(p.entrance_lat_e7),
    lon: degreesText(p.entrance_lon_e7),
    street: p.address?.street ?? '',
    postal_code: p.address?.postal_code ?? '',
    city: p.address?.city ?? '',
    province: p.address?.province ?? '',
    country: p.address?.country ?? '',
    phone: p.phone ?? '',
    email: p.email ?? '',
    website: p.website ?? '',
    vhf: [...(p.vhf_channels ?? [])],
    office_hours: hoursOf(p.office_hours),
    dock_hours: hoursOf(p.dock_hours),
    hours_note: textOf(p.hours_note),
    berths_total_override: p.berths_total_override ?? null,
    transit_berths_override: p.transit_berths_override ?? null,
    max_loa_cm: p.max_loa_cm ?? null,
    depth_entrance_cm: p.depth_entrance_cm ?? null,
    depth_quay_cm: p.depth_quay_cm ?? null,
    depth_min_cm: p.depth_min_cm ?? null,
    depth_max_cm: p.depth_max_cm ?? null,
    seabed: p.seabed ?? '',
    shelter: [...(p.shelter ?? [])],
    access_notes: textOf(p.access_notes),
    lights: textOf(p.lights),
    description: textOf(p.description),
    services: {
      flags: SERVICE_FLAGS.filter((f) => s[f] === true),
      power_amps: [...(s.power_amps ?? [])],
      petrol: s.fuel?.petrol ?? false,
      diesel: s.fuel?.diesel ?? false,
      fuel_hours: textOf(s.fuel?.hours),
      travel_lift_kg: s.travel_lift_kg ?? null,
      crane_kg: s.crane_kg ?? null,
    },
    photos: photosOf(p.photos),
    access_images: photosOf(p.access_images),
    show_map: p.show_map ?? false,
    map_id: p.map_id ?? '',
    show_prices: p.show_prices ?? false,
    show_occupancy: p.show_occupancy ?? false,
    occupancy_categories: [...(p.occupancy_categories ?? [])],
    poi: (p.poi ?? []).map((x: ProfilePoi) => ({ kind: x.kind, x_cm: x.x_cm, y_cm: x.y_cm })),
  };
}

function opt<T>(v: T | null): T | undefined {
  return v === null ? undefined : v;
}

/** The request body of a save. */
export function toInput(v: ProfileValues): MarinaProfileInput {
  const lat = v.lat.trim() === '' ? null : parseDegrees(v.lat, 'lat');
  const lon = v.lon.trim() === '' ? null : parseDegrees(v.lon, 'lon');
  const services: ProfileServices = {};
  for (const f of v.services.flags) services[f] = true;
  if (v.services.power_amps.length > 0) {
    services.power_amps = [...v.services.power_amps].sort(
      (a, b) => a - b,
    ) as ProfileServices['power_amps'];
  }
  const fuelHours = textIn(v.services.fuel_hours);
  if (v.services.petrol || v.services.diesel || Object.keys(fuelHours).length > 0) {
    services.fuel = { petrol: v.services.petrol, diesel: v.services.diesel, hours: fuelHours };
  }
  if (v.services.travel_lift_kg !== null) services.travel_lift_kg = v.services.travel_lift_kg;
  if (v.services.crane_kg !== null) services.crane_kg = v.services.crane_kg;
  const photos = (ps: ProfileValues['photos']): ProfilePhoto[] =>
    ps.map((p, i) => ({ file_id: p.file_id, alt: textIn(p.alt), sort: i }));
  return {
    locales: v.locales.includes('it') ? v.locales : ['it', ...v.locales],
    entrance_lat_e7: opt(lat),
    entrance_lon_e7: opt(lon),
    address: {
      street: v.street.trim(),
      postal_code: v.postal_code.trim(),
      city: v.city.trim(),
      province: v.province.trim().toUpperCase(),
      country: v.country.trim().toUpperCase(),
    },
    phone: v.phone.trim(),
    email: v.email.trim(),
    website: v.website.trim(),
    vhf_channels: v.vhf,
    office_hours: v.office_hours,
    dock_hours: v.dock_hours,
    hours_note: textIn(v.hours_note),
    berths_total_override: opt(v.berths_total_override),
    transit_berths_override: opt(v.transit_berths_override),
    max_loa_cm: opt(v.max_loa_cm),
    depth_entrance_cm: opt(v.depth_entrance_cm),
    depth_quay_cm: opt(v.depth_quay_cm),
    depth_min_cm: opt(v.depth_min_cm),
    depth_max_cm: opt(v.depth_max_cm),
    seabed: v.seabed === '' ? undefined : v.seabed,
    shelter: WINDS.filter((w) => v.shelter.includes(w)),
    access_notes: textIn(v.access_notes),
    lights: textIn(v.lights),
    description: textIn(v.description),
    services,
    photos: photos(v.photos),
    access_images: photos(v.access_images),
    show_map: v.show_map,
    map_id: v.map_id || undefined,
    show_prices: v.show_prices,
    show_occupancy: v.show_occupancy,
    occupancy_categories: v.show_occupancy ? v.occupancy_categories : [],
    poi: v.poi,
  };
}

/** Publishing needs a position, a contact and a name (the server answers 422 profile_incomplete otherwise). */
export function canPublish(p: MarinaProfile): boolean {
  return (p.missing ?? []).length === 0;
}

/** The public page URL with its preview query for the draft. */
export function previewUrl(publicUrl: string, marinaId: string): string {
  const path = publicUrl.replace(/^https?:\/\/[^/]+/, '');
  return `${path}?preview=1&marina=${encodeURIComponent(marinaId)}`;
}
