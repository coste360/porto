// Export wizard "da fatturare" (m5-plan §3.6, §9.1): period and filters → preview totals per VAT and origin with
// warnings → confirm → downloads. The preview is only a preview: the server re-reads the selection with locks
// when the export is created (INV-53), so the confirmed count can be lower.
import type { ChargeOriginType, ExportFilter, ExportPreview } from '@porto/shared/api';

export const EXPORT_STEPS = ['filters', 'preview', 'done'] as const;
export type ExportStep = (typeof EXPORT_STEPS)[number];

export type ExportForm = {
  from: string | null;
  to: string | null;
  customerId?: string;
  originTypes: ChargeOriginType[];
  revenueClass?: string;
};

/** First and last day of the month before `today` (the usual monthly export). */
export function previousMonth(today: string): { from: string; to: string } {
  const [y, m] = today.split('-').map(Number) as [number, number];
  const py = m === 1 ? y - 1 : y;
  const pm = m === 1 ? 12 : m - 1;
  const last = new Date(Date.UTC(py, pm, 0)).getUTCDate();
  const mm = String(pm).padStart(2, '0');
  return { from: `${py}-${mm}-01`, to: `${py}-${mm}-${String(last).padStart(2, '0')}` };
}

/** Validation key of the filters step, or null when the period is usable. */
export function formError(f: ExportForm): string | null {
  if (!f.from || !f.to) return 'exports:wizard.errors.period';
  if (f.from > f.to) return 'exports:wizard.errors.order';
  return null;
}

/** Body of preview/create from the form (only set filters). */
export function toFilter(f: ExportForm): ExportFilter {
  return {
    period_from: f.from ?? '',
    period_to: f.to ?? '',
    ...(f.customerId ? { customer_id: f.customerId } : {}),
    ...(f.originTypes.length ? { origin_types: f.originTypes } : {}),
    ...(f.revenueClass ? { revenue_class: f.revenueClass } : {}),
  };
}

/** The preview can be confirmed when it has charges; warnings do not block (the accountant fixes them). */
export function canConfirm(p: ExportPreview | undefined): boolean {
  return !!p && p.count > 0;
}

/** Σ of the VAT buckets equals the preview total (guard shown as a warning if the server ever disagrees). */
export function vatTotalsMatch(p: ExportPreview): boolean {
  return p.totals_by_vat.reduce((s, b) => s + b.gross_cents, 0) === p.total_cents;
}
