// Length units of the public page (m8-plan §12.1, spec 29): data are integer centimetres, the vessel is typed in
// metres with two decimals and feet are a display-only conversion (1 ft = 30.48 cm).

const CM_PER_FT_X100 = 3048;

/** Feet with one decimal for a length in cm (display only). */
export function cmToFeetText(cm: number, locale = 'it'): string {
  const tenths = Math.round((cm * 1000) / CM_PER_FT_X100);
  const whole = Math.trunc(tenths / 10);
  const frac = Math.abs(tenths % 10);
  return `${whole}${locale.startsWith('en') ? '.' : ','}${frac}`;
}

/** Metres with two decimals for a length in cm. */
export function cmToMetresText(cm: number, locale = 'it'): string {
  const whole = Math.trunc(cm / 100);
  const frac = String(Math.abs(cm % 100)).padStart(2, '0');
  return `${whole}${locale.startsWith('en') ? '.' : ','}${frac}`;
}

/** "12,50 m (41,0 ft)" or "12,50 m" when feet are hidden. */
export function formatLength(cm: number, locale: string, showFeet: boolean): string {
  const m = `${cmToMetresText(cm, locale)} m`;
  return showFeet ? `${m} (${cmToFeetText(cm, locale)} ft)` : m;
}

/** Bounds of the vessel inputs, mirrored from BookingVesselInput in the API contract. */
export const VESSEL_LIMITS_CM = { loa: 10_000, beam: 5_000, draft: 2_000 } as const;
