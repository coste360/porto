import type { Schemas } from '@porto/shared/api';

import { addDays, daysBetween, isIsoDate } from './dates';

// Quote editor (13.2, m3-plan §4.3, D28) as data: 1–3 alternatives, each with 1–2 segments covering the stay and
// an optional hold until the quote expiry.

type S = Schemas;

type Prefill = {
  target?: S['PlacementInput'];
  arrival?: string;
  departure?: string;
  vesselId?: string;
  solution?: S['OccupancySolution'];
};
export const MAX_OPTIONS = 3;

export type OptionDraft = {
  label: string;
  hold: boolean;
  /** One target for the whole stay, or two with the change of place on `splitOn`. */
  targets: [S['PlacementInput'] | null] | [S['PlacementInput'] | null, S['PlacementInput'] | null];
  splitOn: string | null;
};

export type QuoteDraft = {
  kind: S['QuoteKind'];
  term: S['ContractTerm'];
  customerId: string | null;
  vesselId: string | null;
  startsOn: string | null;
  endsOn: string | null;
  validUntil: string | null;
  notes: string;
  options: OptionDraft[];
};

export function emptyOption(target: S['PlacementInput'] | null = null): OptionDraft {
  return { label: '', hold: false, targets: [target], splitOn: null };
}

/** An option from a search solution: its segments become the targets (and the split night). */
export function optionFromSolution(s: S['OccupancySolution']): OptionDraft {
  const target = (seg: S['SolutionSegment']): S['PlacementInput'] => ({
    resource_type: seg.resource_type,
    resource_id: seg.resource_id,
    ...(seg.x_cm !== undefined ? { x_cm: seg.x_cm } : {}),
    ...(seg.y_cm !== undefined ? { y_cm: seg.y_cm } : {}),
    ...(seg.rotation_dd !== undefined ? { rotation_dd: seg.rotation_dd } : {}),
  });
  const [a, b] = s.segments;
  if (a && b)
    return { label: '', hold: true, targets: [target(a), target(b)], splitOn: b.first_night };
  return { label: '', hold: true, targets: [a ? target(a) : null], splitOn: null };
}

export type QuoteDraftError =
  | 'desk:quote.errors.customer'
  | 'desk:quote.errors.vessel'
  | 'desk:quote.errors.dates'
  | 'desk:quote.errors.options'
  | 'desk:quote.errors.placement'
  | 'desk:quote.errors.split';

function optionPlacements(
  o: OptionDraft,
  startsOn: string,
  endsOn: string,
): S['SegmentInput'][] | null {
  const last = addDays(endsOn, -1);
  if (o.targets.length === 1) {
    const t = o.targets[0];
    return t ? [{ ...t, first_night: startsOn, last_night: last }] : null;
  }
  const [t1, t2] = o.targets;
  if (!t1 || !t2 || !isIsoDate(o.splitOn)) return null;
  if (o.splitOn <= startsOn || o.splitOn > last) return null;
  return [
    { ...t1, first_night: startsOn, last_night: addDays(o.splitOn, -1) },
    { ...t2, first_night: o.splitOn, last_night: last },
  ];
}

/** createQuote / updateQuote body, or the i18n key of the first problem. */
export function quoteInput(
  d: QuoteDraft,
): { ok: true; body: S['QuoteInput'] } | { ok: false; error: QuoteDraftError } {
  if (!d.customerId) return { ok: false, error: 'desk:quote.errors.customer' };
  if (!d.vesselId) return { ok: false, error: 'desk:quote.errors.vessel' };
  if (!isIsoDate(d.startsOn) || !isIsoDate(d.endsOn) || daysBetween(d.startsOn, d.endsOn) < 1)
    return { ok: false, error: 'desk:quote.errors.dates' };
  if (d.options.length < 1 || d.options.length > MAX_OPTIONS)
    return { ok: false, error: 'desk:quote.errors.options' };
  const options: S['QuoteOptionInput'][] = [];
  for (const o of d.options) {
    const placements = optionPlacements(o, d.startsOn, d.endsOn);
    if (!placements)
      return {
        ok: false,
        error:
          o.targets.length === 2 && o.targets.every(Boolean)
            ? 'desk:quote.errors.split'
            : 'desk:quote.errors.placement',
      };
    options.push({
      hold: o.hold,
      placements,
      ...(o.label.trim() ? { label: o.label.trim() } : {}),
    });
  }
  const body: S['QuoteInput'] = {
    kind: d.kind,
    customer_id: d.customerId,
    vessel_id: d.vesselId,
    starts_on: d.startsOn,
    ends_on: d.endsOn,
    channel: 'desk',
    options,
  };
  if (d.kind === 'mooring') body.term = d.term;
  if (isIsoDate(d.validUntil)) body.valid_until = d.validUntil;
  if (d.notes.trim()) body.notes = d.notes.trim();
  return { ok: true, body };
}

/** Draft of an existing quote, for the editor of a draft quote. */
export function draftFromQuote(q: S['Quote']): QuoteDraft {
  return {
    kind: q.kind,
    term: q.term ?? 'annual',
    customerId: q.customer.id,
    vesselId: q.vessel.id,
    startsOn: q.starts_on,
    endsOn: q.ends_on,
    validUntil: q.valid_until,
    notes: q.notes ?? '',
    options: q.options.map((o) => {
      const t = (p: S['QuotePlacement']): S['PlacementInput'] => ({
        resource_type: p.resource_type,
        resource_id: p.resource_id,
        ...(p.x_cm !== undefined ? { x_cm: p.x_cm } : {}),
        ...(p.y_cm !== undefined ? { y_cm: p.y_cm } : {}),
        ...(p.rotation_dd !== undefined ? { rotation_dd: p.rotation_dd } : {}),
      });
      const [a, b] = [...o.placements].sort((x, y) => x.seq - y.seq);
      return {
        label: o.label,
        hold: o.hold,
        targets: a && b ? [t(a), t(b)] : [a ? t(a) : null],
        splitOn: b ? b.first_night : null,
      };
    }),
  };
}

/**
 * Time left before a quote expires, in whole days and hours (floor), or null once expired. `now` and
 * `expiresAt` are instants; integer milliseconds only.
 */
export function expiryCountdown(
  expiresAt: string,
  now: Date,
): { days: number; hours: number } | null {
  const left = Date.parse(expiresAt) - now.getTime();
  if (!Number.isFinite(left) || left <= 0) return null;
  const hoursTotal = Math.floor(left / 3_600_000);
  return { days: Math.floor(hoursTotal / 24), hours: hoursTotal % 24 };
}

/** Initial quote draft of a desk prefill (search solution, map target, stay, vessel). */
export function quoteDraftFromPrefill(prefill: Prefill | undefined, today: string): QuoteDraft {
  const startsOn = prefill?.arrival ?? prefill?.solution?.segments[0]?.first_night ?? today;
  return {
    kind: 'transit',
    term: 'annual',
    customerId: null,
    vesselId: prefill?.vesselId ?? null,
    startsOn,
    endsOn: prefill?.departure ?? addDays(startsOn, 1),
    validUntil: null,
    notes: '',
    options: [
      prefill?.solution
        ? optionFromSolution(prefill.solution)
        : emptyOption(prefill?.target ?? null),
    ],
  };
}
