import type {
  FinalAccount,
  FinalAccountLine,
  Timesheet,
  WorkOrderAction,
  WorkOrderStatus,
} from '@porto/shared/api';
import { formatQtyMilli } from '@porto/shared/lib/format';

// Pure helpers of the work order pages (m10-plan §5.2, §9, §14.2): the actions the state table allows, the
// actions that ask for a reason, the selection of entries to approve and the grouping of the final account.

/** The transitions of the state table that the panel offers (the final billing moves to invoiced). */
const ACTIONS: Record<WorkOrderStatus, readonly WorkOrderAction[]> = {
  planned: ['start', 'cancel'],
  in_progress: ['hold', 'complete', 'cancel'],
  on_hold: ['resume', 'cancel'],
  completed: ['reopen', 'deliver'],
  delivered: [],
  invoiced: ['deliver'],
  cancelled: [],
};

/** Allowed actions of an order; deliver on an invoiced order only when not delivered yet. */
export function actionsFor(
  status: WorkOrderStatus,
  deliveredAt?: string | null,
): readonly WorkOrderAction[] {
  const list = ACTIONS[status] ?? [];
  return status === 'invoiced' && deliveredAt ? [] : list;
}

/** Actions that ask for a reason (cancel requires it, hold records it). */
export function needsReason(action: WorkOrderAction): 'required' | 'optional' | null {
  if (action === 'cancel') return 'required';
  if (action === 'hold') return 'optional';
  return null;
}

/** Entries that can be approved (submitted), with their versions. */
export function approvable(entries: readonly Timesheet[]): { id: string; version: number }[] {
  return entries
    .filter((e) => e.status === 'submitted')
    .map((e) => ({ id: e.id, version: e.version }));
}

/** The selected entries still approvable (a refresh may have approved some already). */
export function selectedApprovable(
  entries: readonly Timesheet[],
  selected: ReadonlySet<string>,
): { id: string; version: number }[] {
  return approvable(entries).filter((e) => selected.has(e.id));
}

export type AccountGroup = {
  source: FinalAccountLine['source'];
  lines: FinalAccountLine[];
  revenue: number;
  cost: number;
};

/** Account lines grouped by source (labour, material, service, external), Σ revenue and cost per group. */
export function groupAccount(account: FinalAccount): AccountGroup[] {
  const order: FinalAccountLine['source'][] = ['labour', 'material', 'service', 'external'];
  return order
    .map((source) => {
      const lines = account.lines.filter((l) => l.source === source);
      return {
        source,
        lines,
        revenue: lines.reduce((s, l) => s + l.revenue_cents, 0),
        cost: lines.reduce((s, l) => s + l.cost_cents, 0),
      };
    })
    .filter((g) => g.lines.length > 0);
}

/** Hours in milli as a decimal string (8400 → "8,4"). */
export function hoursText(milli: number, locale = 'it'): string {
  return formatQtyMilli(milli, locale);
}

/** Parses a decimal hour input ("4,25" or "4.25") into milli; null when not a positive 0.25 step. */
export function parseHours(text: string): number | null {
  const v = text.trim().replace(',', '.');
  if (!/^\d+(\.\d{1,3})?$/.test(v)) return null;
  const [int, frac = ''] = v.split('.');
  const milli = Number(int) * 1000 + Number((frac + '000').slice(0, 3));
  if (milli <= 0 || milli % 250 !== 0) return null;
  return milli;
}

/** Parses a decimal quantity ("6", "1,5") into milli; null when not positive. */
export function parseQty(text: string): number | null {
  const v = text.trim().replace(',', '.');
  if (!/^\d+(\.\d{1,3})?$/.test(v)) return null;
  const [int, frac = ''] = v.split('.');
  const milli = Number(int) * 1000 + Number((frac + '000').slice(0, 3));
  return milli > 0 ? milli : null;
}
