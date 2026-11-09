// Types shared by the lenses (spec 9.2, m3-plan §8.2): a lens is a pure function of one day-view subject.
import type { components } from '../../api/schema';
import type { HarborToken } from './tokens';

type S = components['schemas'];
export type DayView = S['DayView'];
export type DayElement = S['DayElement'];
export type AreaPlacement = S['AreaPlacement'];

/** Glyph symbols drawn by LensFill (GlyphDefs): the state never depends on colour only. */
export type GlyphId =
  | 'star'
  | 'suitcase'
  | 'clock'
  | 'release'
  | 'wrench'
  | 'lock'
  | 'arrowIn'
  | 'arrowOut'
  | 'calendar'
  | 'bolt'
  | 'drop'
  | 'alert';

export type LensId =
  | 'occupancy'
  | 'contractType'
  | 'vesselType'
  | 'presence'
  | 'deadlines'
  | 'fitness'
  | 'utilities'
  | 'anomalies';

/** Legend keys: `occupancyEnums:lens.legend.<key>`. */
export type LegendKey =
  | 'free'
  | 'hold'
  | 'contract'
  | 'transit'
  | 'released'
  | 'blocked'
  | 'closed'
  | 'maintenance'
  | 'owner'
  | 'annual'
  | 'seasonal'
  | 'monthly'
  | 'period'
  | 'in'
  | 'out'
  | 'awayLong'
  | 'arrival'
  | 'departure'
  | 'ok'
  | 'warn'
  | 'bad'
  | 'override'
  | 'power'
  | 'water'
  | 'none'
  | 'unrecorded'
  | 'noMovement'
  | 'anomaly'
  | 'monohull_motor'
  | 'monohull_sail'
  | 'catamaran'
  | 'trimaran'
  | 'rib'
  | 'gozzo'
  | 'other';

export type LensStyle = {
  /** The --harbor-* token of the fill (TOKEN_CLASSES maps it to classes). */
  colorToken: HarborToken;
  glyph?: GlyphId;
  /** Short language-neutral text (percent, amperes, count, contract initial). */
  badge?: string;
  legendKey: LegendKey;
};

/**
 * What a lens looks at: a day element (berth, land resource) or an area placement (one vessel in a dynamic
 * area), both reduced to the same fields.
 */
export type LensSubject = Pick<
  DayElement,
  | 'status'
  | 'kind'
  | 'contract'
  | 'vessel'
  | 'customer'
  | 'presence'
  | 'release'
  | 'arrival_today'
  | 'departure_today'
  | 'fit'
  | 'deadlines'
  | 'utilities'
  | 'anomalies'
>;

export type LensContext = {
  /** Night of the day view (YYYY-MM-DD). */
  date: string;
  /** occupancy_settings.absent_days_threshold (default 7). */
  absentDaysThreshold: number;
  /** Contract end within this many days is a deadline warning (default 30). */
  deadlineWarnDays: number;
};

export const DEFAULT_LENS_CONTEXT: Omit<LensContext, 'date'> = {
  absentDaysThreshold: 7,
  deadlineWarnDays: 30,
};

export type Lens = (el: LensSubject, ctx: LensContext) => LensStyle;
