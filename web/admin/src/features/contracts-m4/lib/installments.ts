// Pure helpers of the instalment screens (spec 13.7, m4-plan §3.3): status display, ageing of overdue items,
// plan checks (INV-31: Σ instalments = total, shown, never computed here) and the settle dialog mapping. The
// server computes plans and settles; these only format and validate what the office types.
import type { Schemas } from '@porto/shared/api';

type Installment = Schemas['Installment'];
type Status = Schemas['InstallmentStatus'];
type Method = Schemas['SettlementMethod'];

export const SETTLEMENT_METHODS: readonly Method[] = [
  'bank_transfer',
  'cash',
  'card_pos',
  'cheque',
  'other',
];

export const INSTALLMENT_FILTER_STATUSES: readonly Schemas['InstallmentFilterStatus'][] = [
  'scheduled',
  'issued',
  'overdue',
  'paid',
  'cancelled',
];

export function installmentVariant(
  status: Status,
  overdue: boolean,
): 'default' | 'secondary' | 'outline' | 'destructive' {
  if (overdue) return 'destructive';
  switch (status) {
    case 'paid':
      return 'default';
    case 'issued':
    case 'partially_paid':
      return 'secondary';
    default:
      return 'outline';
  }
}

/** i18n key of an instalment status (overdue is computed, m4-plan §3.3). */
export function installmentStatusKey(status: Status, overdue: boolean): string {
  return overdue ? 'docsEnums:installmentStatus.overdue' : `docsEnums:installmentStatus.${status}`;
}

/** Days between two YYYY-MM-DD dates (b − a), calendar arithmetic in UTC. */
export function daysBetween(a: string, b: string): number {
  const ms = Date.parse(`${b}T00:00:00Z`) - Date.parse(`${a}T00:00:00Z`);
  return Math.round(ms / 86_400_000);
}

export type AgeingBucket = 'd0_30' | 'd31_60' | 'd61_90' | 'd90plus';

/** Ageing bucket of an overdue instalment on `today`, or null when it is not overdue. */
export function ageingBucket(due_on: string, today: string): AgeingBucket | null {
  const days = daysBetween(due_on, today);
  if (days <= 0) return null;
  if (days <= 30) return 'd0_30';
  if (days <= 60) return 'd31_60';
  if (days <= 90) return 'd61_90';
  return 'd90plus';
}

/** Overdue ageing of a page of instalments: count and cents per bucket (integer cents, no floats). */
export function ageing(
  items: readonly Pick<Installment, 'due_on' | 'amount_cents' | 'overdue' | 'status'>[],
  today: string,
): Record<AgeingBucket, { count: number; cents: number }> {
  const out: Record<AgeingBucket, { count: number; cents: number }> = {
    d0_30: { count: 0, cents: 0 },
    d31_60: { count: 0, cents: 0 },
    d61_90: { count: 0, cents: 0 },
    d90plus: { count: 0, cents: 0 },
  };
  for (const it of items) {
    if (!it.overdue && it.status !== 'issued') continue;
    const b = ageingBucket(it.due_on, today);
    if (!b) continue;
    out[b].count += 1;
    out[b].cents += it.amount_cents;
  }
  return out;
}

/** Which bulk actions apply to a selection (issue only scheduled, remind and settle only issued/scheduled). */
export function bulkActions(selected: readonly Pick<Installment, 'status'>[]) {
  const any = selected.length > 0;
  return {
    issue: any && selected.every((i) => i.status === 'scheduled'),
    remind: any && selected.every((i) => i.status === 'issued'),
    settle: any && selected.every((i) => i.status === 'issued' || i.status === 'scheduled'),
  };
}

export const BULK_MAX = 500;

/** Plan check shown under every plan: the server's sum must equal the total to the cent (INV-31). */
export function planBalanced(plan: Pick<Schemas['InstallmentPlan'], 'total_cents' | 'sum_cents'>) {
  return plan.total_cents === plan.sum_cents;
}

/** Σ of live instalments of a plan (cancelled rows are history). */
export function liveSum(items: readonly Pick<Installment, 'amount_cents' | 'status'>[]): number {
  return items.filter((i) => i.status !== 'cancelled').reduce((s, i) => s + i.amount_cents, 0);
}

// ---- settle dialog ----

export type SettleForm = {
  method: Method | '';
  reference: string;
  settledOn: string | null;
};

export type SettleMapping =
  | { ok: true; body: Schemas['SettleInput'] }
  | { ok: false; error: 'method' | 'reference' | 'date' };

/**
 * Maps the settle dialog to the API body. A bank transfer or a cheque needs a reference (CRO, cheque number); the
 * settlement date cannot be in the future.
 */
export function settleBody(form: SettleForm, today: string): SettleMapping {
  if (!form.method) return { ok: false, error: 'method' };
  const reference = form.reference.trim();
  if ((form.method === 'bank_transfer' || form.method === 'cheque') && !reference) {
    return { ok: false, error: 'reference' };
  }
  if (form.settledOn && form.settledOn > today) return { ok: false, error: 'date' };
  return {
    ok: true,
    body: {
      method: form.method,
      ...(reference ? { reference: reference.slice(0, 140) } : {}),
      settled_on: form.settledOn ?? today,
    },
  };
}

export type BulkSettleMapping =
  | { ok: true; body: Schemas['BulkSettleInput'] }
  | { ok: false; error: 'method' | 'reference' | 'date' | 'empty' | 'too_many' };

export function bulkSettleBody(
  items: readonly { id: string; version: number }[],
  form: SettleForm,
  today: string,
): BulkSettleMapping {
  if (items.length === 0) return { ok: false, error: 'empty' };
  if (items.length > BULK_MAX) return { ok: false, error: 'too_many' };
  const one = settleBody(form, today);
  if (!one.ok) return one;
  return {
    ok: true,
    body: { items: items.map(({ id, version }) => ({ id, version })), ...one.body },
  };
}
