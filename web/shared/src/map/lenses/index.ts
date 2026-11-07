// Lenses of the operational view (spec 9.2, m3-plan §8.2, D23): one map, many points of view. A lens is a pure
// function of a day-view subject returning a colour token, an optional glyph and badge, and its legend key.
import { anomaliesLens } from './anomalies';
import { contractTypeLens } from './contractType';
import { deadlinesLens } from './deadlines';
import { fitnessLens } from './fitness';
import { occupancyLens } from './occupancy';
import { presenceLens } from './presence';
import type { Lens, LensId } from './types';
import { utilitiesLens } from './utilities';
import { vesselTypeLens } from './vesselType';

export * from './types';
export * from './tokens';
export * from './legend';
export { daysBetween } from './deadlines';
export { FIT_OK_BP, FIT_WARN_BP } from './fitness';

export const LENSES: Readonly<Record<LensId, Lens>> = {
  occupancy: occupancyLens,
  contractType: contractTypeLens,
  vesselType: vesselTypeLens,
  presence: presenceLens,
  deadlines: deadlinesLens,
  fitness: fitnessLens,
  utilities: utilitiesLens,
  anomalies: anomaliesLens,
};

/** Lenses shown as tabs above the map; the others are under "Altri" (spec 9.2). */
export const PRIMARY_LENSES: readonly LensId[] = [
  'occupancy',
  'contractType',
  'presence',
  'anomalies',
];
export const OTHER_LENSES: readonly LensId[] = ['vesselType', 'deadlines', 'fitness', 'utilities'];
export const LENS_IDS: readonly LensId[] = [...PRIMARY_LENSES, ...OTHER_LENSES];

export function isLensId(v: unknown): v is LensId {
  return typeof v === 'string' && (LENS_IDS as readonly string[]).includes(v);
}

export {
  anomaliesLens,
  contractTypeLens,
  deadlinesLens,
  fitnessLens,
  occupancyLens,
  presenceLens,
  utilitiesLens,
  vesselTypeLens,
};
