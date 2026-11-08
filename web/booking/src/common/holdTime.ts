/** Whole seconds left until an RFC 3339 instant (never negative). */
export function secondsLeft(expiresAt: string, nowMs: number): number {
  const end = Date.parse(expiresAt);
  if (Number.isNaN(end)) return 0;
  return Math.max(0, Math.ceil((end - nowMs) / 1000));
}

/** "m:ss" for a number of seconds. */
export function formatMinutes(seconds: number): string {
  const m = Math.floor(seconds / 60);
  const s = seconds % 60;
  return `${m}:${String(s).padStart(2, '0')}`;
}
