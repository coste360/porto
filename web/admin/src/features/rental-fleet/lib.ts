// Pure helpers of the rental fleet pages (m11-plan §3, §14.1): slot labels, band strip validation (the server
// repeats every rule; the page shows the same refusal earlier), weekly hours rows and kind-dependent formula fields.
import type {
  RentalBand,
  RentalCategory,
  RentalExtraChargeMode,
  RentalFormulaKind,
  RentalFuelMode,
  RentalFuelType,
  RentalLicenceMode,
  RentalOwnership,
  RentalSkipperHoursRow,
  RentalSkipperMode,
  RentalUnitStatus,
} from '@porto/shared/api';

export const SLOTS_PER_DAY = 48;
/** Slots 4-5 (02:00-03:00) are the labels DST removes or repeats: the window and the bands exclude them. */
export const DST_FROM = 4;
export const DST_TO = 6;

export const CATEGORIES: readonly RentalCategory[] = [
  'rib',
  'open',
  'cabin',
  'sail',
  'pedalo',
  'sup',
];
export const LICENCE_MODES: readonly RentalLicenceMode[] = ['never', 'always', 'threshold'];
export const FUEL_TYPES: readonly RentalFuelType[] = ['petrol', 'diesel', 'electric', 'none'];
export const UNIT_STATUSES: readonly RentalUnitStatus[] = ['available', 'maintenance', 'retired'];
export const OWNERSHIPS: readonly RentalOwnership[] = ['marina', 'third_party'];
export const FORMULA_KINDS: readonly RentalFormulaKind[] = [
  'hourly',
  'half_day',
  'day',
  'multi_day',
  'week',
];
export const SKIPPER_MODES: readonly RentalSkipperMode[] = ['none', 'optional', 'required'];
export const FUEL_MODES: readonly RentalFuelMode[] = ['included', 'metered'];
export const CHARGE_MODES: readonly RentalExtraChargeMode[] = ['per_rental', 'per_day'];
export const LATE_STEPS = [15, 30, 60] as const;
export const DEPOSIT_MODES = ['cash', 'card_pos', 'preauth'] as const;
export const WEEKDAYS = [1, 2, 3, 4, 5, 6, 7] as const;

/** "HH:MM" of slot s (48 = "24:00"). */
export function slotLabel(s: number): string {
  const c = Math.min(Math.max(Math.trunc(s), 0), SLOTS_PER_DAY);
  const h = Math.floor(c / 2);
  return `${String(h).padStart(2, '0')}:${c % 2 === 0 ? '00' : '30'}`;
}

/** The slot of an "HH:MM" label (minutes rounded down to the half hour), or null when malformed. */
export function parseSlotLabel(label: string): number | null {
  const m = /^(\d{1,2}):(\d{2})$/.exec(label.trim());
  if (!m) return null;
  const h = Number(m[1]);
  const min = Number(m[2]);
  if (h > 24 || min > 59 || (h === 24 && min !== 0)) return null;
  return h * 2 + (min >= 30 ? 1 : 0);
}

/** Every selectable slot boundary 0..48 with its label (time pickers). */
export const SLOT_OPTIONS: readonly { value: number; label: string }[] = Array.from(
  { length: SLOTS_PER_DAY + 1 },
  (_, s) => ({ value: s, label: slotLabel(s) }),
);

const touchesDst = (from: number, to: number) => from < DST_TO && to > DST_FROM;

export type WindowIssue =
  | 'window_invalid'
  | 'window_dst'
  | 'buffer_range'
  | 'bands_count'
  | 'band_code'
  | 'band_order'
  | 'band_outside'
  | 'band_dst'
  | 'band_gap_lt_buffer';

/** The problems of an open window, its bands and the buffer (m11-plan §3.5); empty when valid. */
export function windowIssues(
  openFrom: number,
  openTo: number,
  bands: readonly Pick<RentalBand, 'code' | 'from_slot' | 'to_slot'>[],
  buffer: number,
): WindowIssue[] {
  const out = new Set<WindowIssue>();
  if (openFrom < 0 || openTo > SLOTS_PER_DAY || openFrom >= openTo) out.add('window_invalid');
  else if (touchesDst(openFrom, openTo)) out.add('window_dst');
  if (buffer < 0 || buffer > 8) out.add('buffer_range');
  if (bands.length < 1 || bands.length > 4) out.add('bands_count');
  const codes = new Set<string>();
  for (const b of bands) {
    if (!/^[a-z0-9_]{1,16}$/.test(b.code) || codes.has(b.code)) out.add('band_code');
    codes.add(b.code);
    if (b.from_slot >= b.to_slot) out.add('band_order');
    else if (b.from_slot < openFrom || b.to_slot > openTo) out.add('band_outside');
    else if (touchesDst(b.from_slot, b.to_slot)) out.add('band_dst');
  }
  const sorted = [...bands].sort((a, b) => a.from_slot - b.from_slot);
  for (let i = 1; i < sorted.length; i++) {
    const prev = sorted[i - 1];
    const cur = sorted[i];
    if (prev && cur && cur.from_slot - prev.to_slot < buffer) out.add('band_gap_lt_buffer');
  }
  return [...out];
}

export type StripCell = { slot: number; kind: 'closed' | 'open' | 'band' | 'dst'; band?: number };

/** The 48 cells of the visual strip: closed, open window, band index, DST labels. */
export function stripCells(
  openFrom: number,
  openTo: number,
  bands: readonly Pick<RentalBand, 'from_slot' | 'to_slot'>[],
): StripCell[] {
  return Array.from({ length: SLOTS_PER_DAY }, (_, slot) => {
    const band = bands.findIndex((b) => slot >= b.from_slot && slot < b.to_slot);
    if (band >= 0) return { slot, kind: 'band', band };
    if (slot >= DST_FROM && slot < DST_TO) return { slot, kind: 'dst' };
    return { slot, kind: slot >= openFrom && slot < openTo ? 'open' : 'closed' };
  });
}

/** Whether the weekly rows are valid: weekday 1..7, from < to within the day, no overlap on a weekday. */
export function hoursValid(rows: readonly RentalSkipperHoursRow[]): boolean {
  const sorted = [...rows].sort((a, b) => a.weekday - b.weekday || a.from_slot - b.from_slot);
  for (let i = 0; i < sorted.length; i++) {
    const r = sorted[i];
    if (!r) continue;
    if (
      r.weekday < 1 ||
      r.weekday > 7 ||
      r.from_slot < 0 ||
      r.to_slot > 48 ||
      r.from_slot >= r.to_slot
    )
      return false;
    const prev = sorted[i - 1];
    if (prev && prev.weekday === r.weekday && prev.to_slot > r.from_slot) return false;
  }
  return true;
}

/** Monday to Saturday over the open window (the default calendar of a new skipper). */
export function defaultHours(openFrom: number, openTo: number): RentalSkipperHoursRow[] {
  return [1, 2, 3, 4, 5, 6].map((weekday) => ({ weekday, from_slot: openFrom, to_slot: openTo }));
}

export type FormulaFields = { band: boolean; slots: boolean; days: boolean; minDays: number };

/** The fields a formula kind uses (m11-plan §3.3). */
export function formulaFields(kind: RentalFormulaKind): FormulaFields {
  switch (kind) {
    case 'hourly':
      return { band: false, slots: true, days: false, minDays: 1 };
    case 'half_day':
      return { band: true, slots: false, days: false, minDays: 1 };
    case 'multi_day':
      return { band: false, slots: false, days: true, minDays: 2 };
    case 'week':
      return { band: false, slots: false, days: true, minDays: 7 };
    default:
      return { band: false, slots: false, days: false, minDays: 1 };
  }
}

/** The licence rule of a unit without a skipper (D7). */
export function licenceRequired(
  mode: RentalLicenceMode,
  engineKwMilli: number,
  thresholdKwMilli: number,
): boolean {
  if (mode === 'always') return true;
  if (mode === 'threshold') return engineKwMilli > thresholdKwMilli;
  return false;
}

/** kW of a milli-kW value for display. */
export const kw = (milli: number) => milli / 1000;

/** Litres or hours of a milli value for display and the milli value of a typed decimal (comma or dot). */
export const fromMilli = (milli: number) => String(milli / 1000);
export function toMilli(text: string): number | null {
  const t = text.trim().replace(',', '.');
  if (t === '') return 0;
  if (!/^\d+(\.\d{1,3})?$/.test(t)) return null;
  return Math.round(Number(t) * 1000);
}

/**
 * The probe range of the price-coverage hint of a formula: a sample date and the range its kind accepts (hourly
 * = the first hour of the window, half day = its band, day = the window, multi day = 2 dates, week = 7 dates).
 */
export function probeRange(
  kind: RentalFormulaKind,
  date: string,
  openFrom: number,
  openTo: number,
  band: Pick<RentalBand, 'from_slot' | 'to_slot'> | undefined,
): { start: { date: string; slot: number }; end: { date: string; slot: number } } | null {
  const plus = (n: number) => {
    const d = new Date(`${date}T00:00:00Z`);
    d.setUTCDate(d.getUTCDate() + n);
    return d.toISOString().slice(0, 10);
  };
  switch (kind) {
    case 'hourly':
      return {
        start: { date, slot: openFrom },
        end: { date, slot: Math.min(openFrom + 2, openTo) },
      };
    case 'half_day':
      return band
        ? { start: { date, slot: band.from_slot }, end: { date, slot: band.to_slot } }
        : null;
    case 'day':
      return { start: { date, slot: openFrom }, end: { date, slot: openTo } };
    case 'multi_day':
      return { start: { date, slot: openFrom }, end: { date: plus(1), slot: openTo } };
    case 'week':
      return { start: { date, slot: openFrom }, end: { date: plus(6), slot: openTo } };
  }
}
