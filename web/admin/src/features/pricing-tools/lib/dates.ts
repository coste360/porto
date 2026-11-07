// Calendar dates `YYYY-MM-DD` as day numbers (UTC epoch days): integer arithmetic, no timezone shift.

const ISO = /^(\d{4})-(\d{2})-(\d{2})$/;

function epochDay(iso: string): number | null {
  const m = ISO.exec(iso);
  if (!m) return null;
  const ms = Date.UTC(Number(m[1]), Number(m[2]) - 1, Number(m[3]));
  return Number.isNaN(ms) ? null : Math.round(ms / 86_400_000);
}

/** Nights of the stay [arrival, departure) (ADR 0002), or null when a date is missing or invalid. */
export function nightsBetween(arrival: string | null, departure: string | null): number | null {
  if (!arrival || !departure) return null;
  const a = epochDay(arrival);
  const d = epochDay(departure);
  return a === null || d === null ? null : d - a;
}

/** `iso` plus `days` calendar days. */
export function addDays(iso: string, days: number): string {
  const day = epochDay(iso);
  if (day === null) return iso;
  const d = new Date((day + days) * 86_400_000);
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getUTCFullYear()}-${pad(d.getUTCMonth() + 1)}-${pad(d.getUTCDate())}`;
}
