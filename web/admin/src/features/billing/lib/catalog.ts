// Enum catalogues and small helpers shared by the billing pages (order of the filters and tables).
import type {
  AgeingAmounts,
  AgeingBucket,
  BillingSettings,
  BillingSettingsInput,
  ChargeOriginType,
  PaymentMethod,
  ReminderLevel,
  WorkItem,
  WorkItemKind,
  components,
} from '@porto/shared/api';

import type { CountedMethod } from './cash';

type OriginItemKey = components['schemas']['OriginItemKey'];
type TransitPaymentInput = components['schemas']['TransitPaymentInput'];

export const ORIGIN_TYPES: readonly ChargeOriginType[] = [
  'installment',
  'holder_installment',
  'transit',
  'contract_trueup',
  'counter_sale',
  'termination',
  'deposit_retention',
  'manual',
  'adjustment',
  'opening_balance',
];

export const PAYMENT_METHODS: readonly PaymentMethod[] = [
  'cash',
  'card_pos',
  'bank_transfer',
  'cheque',
  'other',
  'card_online',
  'wallet',
  'sepa_debit',
  'credit_note',
];

export const BUCKETS: readonly AgeingBucket[] = [
  'not_due',
  'd0_30',
  'd31_60',
  'd61_90',
  'd90_plus',
];

export function bucketCents(a: AgeingAmounts, b: AgeingBucket): number {
  return a[`${b}_cents` as keyof AgeingAmounts];
}

export const WORK_KINDS: readonly WorkItemKind[] = [
  'reminder_approval',
  'charges_to_export',
  'payment_failed',
  'late_payment',
  'overpayment',
  'sepa_failed',
  'billing_item_missing',
  'billing_target_mismatch',
  'arrears_paid',
  'deadline',
  'dispute',
  'capture_failed',
  'channel_modify',
];

/** Groups work items by kind in the catalogue order (only kinds with items). */
export function groupByKind(items: readonly WorkItem[]): [WorkItemKind, WorkItem[]][] {
  const by = new Map<WorkItemKind, WorkItem[]>();
  for (const i of items) by.set(i.kind, [...(by.get(i.kind) ?? []), i]);
  return WORK_KINDS.filter((k) => by.has(k)).map((k) => [k, by.get(k)!]);
}

export const ORIGIN_ITEM_KEYS: readonly OriginItemKey[] = [
  'management_fee',
  'common_advance',
  'common_balance',
  'transfer_fee',
  'termination_penalty',
  'termination_refund',
  'deposit_retention',
  'opening_balance',
  'contract_trueup_fallback',
];

export const REMINDER_LEVELS: readonly ReminderLevel[] = [
  'pre_due',
  'due',
  'courteous',
  'formal',
  'final_notice',
];

/** D13 defaults, used when the stored policy lacks a level. */
const DEFAULT_DAYS: Record<ReminderLevel, number> = {
  pre_due: -7,
  due: 0,
  courteous: 7,
  formal: 21,
  final_notice: 45,
};

/** Input of the PUT from the stored settings (read-only fields dropped, every level present). */
export function toSettingsInput(s: BillingSettings): BillingSettingsInput {
  const levels = REMINDER_LEVELS.map(
    (level) =>
      s.reminder_policy.levels.find((l) => l.level === level) ?? {
        level,
        days: DEFAULT_DAYS[level],
        enabled: false,
      },
  );
  return {
    default_due_days: s.default_due_days,
    auto_payment_request: s.auto_payment_request,
    reminder_policy: { levels },
    reminder_hour: s.reminder_hour,
    arrears_policy: s.arrears_policy,
    export_reminder_days: s.export_reminder_days,
    fiscal_driver: s.fiscal_driver,
    receipt_footer: s.receipt_footer,
    sepa_lead_days: s.sepa_lead_days,
    origin_items: s.origin_items,
    fiscal_bridge_device_id: s.fiscal_bridge_device_id ?? null,
  };
}

/** Base64 of a file's bytes (chunked, no data: prefix). */
export async function fileToBase64(file: Blob): Promise<string> {
  const bytes = new Uint8Array(await file.arrayBuffer());
  let bin = '';
  for (let i = 0; i < bytes.length; i += 0x8000)
    bin += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
  return btoa(bin);
}

/** Format of an uploaded file from its name. */
export function formatOf(name: string): 'csv' | 'xlsx' {
  return name.toLowerCase().endsWith('.xlsx') ? 'xlsx' : 'csv';
}

export type DeskPaymentValue = {
  collect: boolean;
  method: CountedMethod;
  amountCents: number | null;
  fiscalDocument: boolean;
};

export function defaultDeskPayment(totalCents: number): DeskPaymentValue {
  return { collect: false, method: 'cash', amountCents: totalCents, fiscalDocument: false };
}

/** The `payment` / `fiscal_document` part of ManualConfirmation (empty when not collecting). */
export function deskPaymentBody(v: DeskPaymentValue): {
  payment?: TransitPaymentInput;
  fiscal_document?: boolean;
} {
  if (!v.collect) return {};
  return {
    payment: {
      method: v.method,
      ...(v.amountCents && v.amountCents > 0 ? { amount_cents: v.amountCents } : {}),
    },
    ...(v.fiscalDocument ? { fiscal_document: true } : {}),
  };
}
