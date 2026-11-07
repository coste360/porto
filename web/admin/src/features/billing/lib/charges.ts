// Charge list views and detail actions (m5-plan §3.2, §3.5, §9.1). The server enforces every rule (triggers,
// 409 charge_immutable / use_edit); the UI only offers the action that fits the state.
import type { Charge, ChargeFilters } from '@porto/shared/api';

export const CHARGE_VIEWS = ['all', 'open', 'overdue', 'toInvoice', 'adjustments'] as const;
export type ChargeView = (typeof CHARGE_VIEWS)[number];

/** Saved views of the list (Da incassare, Scaduti, Da fatturare, Rettifiche). */
export function viewFilters(view: ChargeView): ChargeFilters {
  switch (view) {
    case 'open':
      return { payment_status: 'open' };
    case 'overdue':
      return { overdue: true };
    case 'toInvoice':
      return { invoicing_status: 'to_invoice' };
    case 'adjustments':
      return { origin_type: 'adjustment' };
    default:
      return {};
  }
}

export type BadgeVariant = 'default' | 'secondary' | 'outline' | 'destructive';

export function paymentVariant(c: Pick<Charge, 'payment_status' | 'overdue'>): BadgeVariant {
  if (c.payment_status === 'paid') return 'default';
  if (c.payment_status === 'cancelled') return 'outline';
  return c.overdue ? 'destructive' : 'secondary';
}

export function invoicingVariant(s: Charge['invoicing_status']): BadgeVariant {
  return s === 'to_invoice' ? 'secondary' : s === 'exported' ? 'outline' : 'default';
}

export type ChargeActions = {
  edit: boolean;
  cancel: boolean;
  adjust: boolean;
  invoiceRef: boolean;
  collect: boolean;
  requestPayment: boolean;
};

/** Actions of the detail page for a charge in its current state. */
export function chargeActions(c: Charge): ChargeActions {
  const live = c.payment_status !== 'cancelled';
  const untouched =
    c.invoicing_status === 'to_invoice' &&
    c.paid_amount_cents === 0 &&
    c.credited_cents === 0 &&
    (c.matches?.length ?? 0) === 0 &&
    (c.offsets?.length ?? 0) === 0;
  const positive = c.total_cents > 0;
  const open = live && positive && c.residual_cents > 0;
  return {
    edit: live && untouched,
    cancel: live && untouched && c.payment_status === 'open',
    adjust: live && !untouched,
    invoiceRef: live && c.invoicing_status !== 'to_invoice',
    collect: open,
    requestPayment: open && !!c.customer,
  };
}
