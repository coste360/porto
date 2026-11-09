import type {
  RentalBookingQuoteOption,
  RentalFormula,
  RentalPlanner,
  RentalPlannerView,
} from '@porto/shared/api';
import {
  addDays,
  daysBetween,
  slotLabel,
  type PlannerBand,
  type PlannerGridRow,
  type PlannerGridSegment,
  type SlotRange,
} from '@porto/shared/planner';

/** Dates shown by a view: one day (hours), a week (half days), two weeks (days). */
export function viewDays(view: RentalPlannerView): number {
  return view === 'hours' ? 1 : view === 'half_days' ? 7 : 14;
}

/** The window [from, to] of a view starting at `from`. */
export function viewWindow(view: RentalPlannerView, from: string): { from: string; to: string } {
  return { from, to: addDays(from, viewDays(view) - 1) };
}

export type FormulaKind = RentalFormula['kind'];

/** The formula kind (and band) a range suggests (m11-plan §14.2): the exact band of a date = half day, the open
 * window of one date = day, several dates = week from 7 dates or multi-day, anything else = hourly. */
export function suggestKind(
  range: SlotRange,
  openFrom: number,
  openTo: number,
  bands: readonly PlannerBand[],
): { kind: FormulaKind; band?: string } {
  const dates = daysBetween(range.start.date, range.end.date) + 1;
  if (dates > 1) return { kind: dates >= 7 ? 'week' : 'multi_day' };
  const band = bands.find((b) => b.from_slot === range.start.slot && b.to_slot === range.end.slot);
  if (band) return { kind: 'half_day', band: band.code };
  if (range.start.slot === openFrom && range.end.slot === openTo) return { kind: 'day' };
  return { kind: 'hourly' };
}

/** The formula of the suggested kind (and band) among the active ones of a unit category, if any. */
export function suggestFormula(
  formulas: readonly RentalFormula[],
  category: string | undefined,
  s: { kind: FormulaKind; band?: string },
): RentalFormula | undefined {
  const fits = (f: RentalFormula) =>
    f.active &&
    (f.categories.length === 0 ||
      (category !== undefined && f.categories.includes(category as never)));
  const same = formulas.filter(
    (f) => fits(f) && f.kind === s.kind && (s.kind !== 'half_day' || f.band_code === s.band),
  );
  if (same.length > 0) return same[0];
  if (s.kind === 'week') return suggestFormula(formulas, category, { kind: 'multi_day' });
  return undefined;
}

/** "HH:MM" of a point, "date HH:MM" when the range spans dates. */
export function rangeText(r: SlotRange): string {
  if (r.start.date === r.end.date) return `${slotLabel(r.start.slot)}-${slotLabel(r.end.slot)}`;
  return `${r.start.date} ${slotLabel(r.start.slot)} → ${r.end.date} ${slotLabel(r.end.slot)}`;
}

type Labels = {
  status: (status: string) => string;
  kind: (kind: string) => string;
  reason: (reason: string) => string;
};

/** Grid rows and segments of a planner response: units grouped by category, then the skippers (read only). */
export function gridOf(
  p: RentalPlanner,
  labels: Labels,
): { rows: PlannerGridRow[]; segments: PlannerGridSegment[] } {
  const units = p.rows.filter((r) => r.kind === 'unit');
  const order = [...units].sort((a, b) => (a.category ?? '').localeCompare(b.category ?? ''));
  const rows: PlannerGridRow[] = [
    ...order.map((r) => ({
      id: r.id,
      label: r.code,
      sublabel: r.status === 'maintenance' ? labels.status('maintenance') : undefined,
      group: r.category,
      readOnly: r.status !== 'available',
    })),
    ...p.rows
      .filter((r) => r.kind === 'skipper')
      .map((r) => ({ id: r.id, label: r.code, group: 'skipper', readOnly: true })),
  ];
  const segments: PlannerGridSegment[] = [];
  for (const r of p.rows) {
    r.segments.forEach((s, i) => {
      const range = { start: s.from, end: s.to };
      const what = s.booking_id
        ? `${s.number ?? ''} ${labels.status(s.status ?? '')}`.trim()
        : s.kind === 'block'
          ? labels.reason(s.reason ?? 'other')
          : labels.kind(s.kind);
      segments.push({
        key: `${r.id}-${i}`,
        rowId: r.id,
        bookingId: s.kind === 'buffer' ? undefined : (s.booking_id ?? undefined),
        blockId: s.block_id ?? undefined,
        kind: s.kind,
        status: s.status ?? undefined,
        range,
        label: s.kind === 'buffer' ? '' : (s.number ?? what),
        ariaLabel: `${r.code}: ${what}, ${rangeText(range)}`,
      });
    });
  }
  return { rows, segments };
}

/** The formula preselected among the quote options: the suggested one when it prices, else the cheapest. */
export function preselect(
  options: readonly RentalBookingQuoteOption[],
  suggested?: string,
): string | undefined {
  const ok = options.filter((o) => o.ok);
  if (suggested && ok.some((o) => o.formula_id === suggested)) return suggested;
  return [...ok].sort((a, b) => a.total_cents - b.total_cents)[0]?.formula_id;
}
