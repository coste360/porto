// Calendar dates `YYYY-MM-DD` of the desk (ADR 0002: the night is the occupancy unit). Integer day arithmetic
// on UTC midnights only, so no timezone ever shifts a date.

const ISO = /^(\d{4})-(\d{2})-(\d{2})$/;
const DAY_MS = 86_400_000;

export function isIsoDate(s: string | null | undefined): s is string {
  const m = s ? ISO.exec(s) : null;
  if (!m) return false;
  const d = new Date(Date.UTC(Number(m[1]), Number(m[2]) - 1, Number(m[3])));
  return d.getUTCFullYear() === Number(m[1]) && d.getUTCMonth() === Number(m[2]) - 1;
}

function toDays(iso: string): number {
  const m = ISO.exec(iso);
  if (!m) throw new RangeError(`not a date: ${iso}`);
  return Date.UTC(Number(m[1]), Number(m[2]) - 1, Number(m[3])) / DAY_MS;
}

function fromDays(days: number): string {
  const d = new Date(days * DAY_MS);
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getUTCFullYear()}-${pad(d.getUTCMonth() + 1)}-${pad(d.getUTCDate())}`;
}

export function addDays(iso: string, n: number): string {
  return fromDays(toDays(iso) + n);
}

/** Days from a to b (b − a); the number of nights of the stay [a, b). */
export function daysBetween(a: string, b: string): number {
  return toDays(b) - toDays(a);
}

/** The last night of a stay [arrival, departure). */
export function lastNightOf(departure: string): string {
  return addDays(departure, -1);
}

/** Same day of month n months later, clamped to the month end (31 Jan + 1 month = 28/29 Feb). */
export function addMonths(iso: string, n: number): string {
  const m = ISO.exec(iso);
  if (!m) throw new RangeError(`not a date: ${iso}`);
  const total = Number(m[1]) * 12 + (Number(m[2]) - 1) + n;
  const year = Math.floor(total / 12);
  const month = total - year * 12;
  const lastDay = new Date(Date.UTC(year, month + 1, 0)).getUTCDate();
  const day = Math.min(Number(m[3]), lastDay);
  const pad = (v: number) => String(v).padStart(2, '0');
  return `${year}-${pad(month + 1)}-${pad(day)}`;
}

/** Inclusive list of nights [from, to]. */
export function nightsOf(from: string, to: string): string[] {
  const out: string[] = [];
  for (let d = toDays(from); d <= toDays(to); d++) out.push(fromDays(d));
  return out;
}

/** Weekday 0 (Sunday) .. 6 of a date, for the planner header. */
export function weekdayOf(iso: string): number {
  return new Date(toDays(iso) * DAY_MS).getUTCDay();
}
