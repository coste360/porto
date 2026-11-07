// Default payment/renewal/cancellation policy editor (spec 13.7, m4-plan §3.3, D3): form state ↔ policy v2 JSON.
// Integers only (bp, cents, days); the server validates the policy again.
import type { ContractPolicy, Schemas } from '@porto/shared/api';

type Mode = Schemas['PaymentSchedule']['mode'];

export type PolicyForm = {
  depositKind: 'none' | 'bp' | 'cents';
  depositBp: number | null;
  depositCents: number | null;
  depositDueDays: string;
  mode: Mode;
  everyMonths: string;
  count: string;
  firstOffsetMonths: string;
  dueOffsetDays: string;
  issueDaysBefore: string;
  roundingUnit: '1' | '100' | '1000';
  earlyEnabled: boolean;
  earlyDiscountBp: number | null;
  earlyPayByDays: string;
  tacit: boolean;
  noticeDays: string;
  cancelNoticeDays: string;
  refund: 'pro_rata' | 'none';
  penaltyBp: number | null;
};

const s = (n: number | undefined, d: number) => String(n ?? d);

export function policyForm(p: ContractPolicy | undefined): PolicyForm {
  const pay = p?.payment ?? {};
  const sch = pay.schedule;
  const dep = pay.deposit ?? null;
  return {
    depositKind: !dep ? 'none' : dep.cents !== undefined ? 'cents' : 'bp',
    depositBp: dep?.bp ?? 3000,
    depositCents: dep?.cents ?? null,
    depositDueDays: s(pay.deposit_due_days, 7),
    mode: sch?.mode ?? 'single',
    everyMonths: s(sch?.every_months, 3),
    count: s(sch?.count, 4),
    firstOffsetMonths: s(sch?.first_due_offset_months, 1),
    dueOffsetDays: s(sch?.due_offset_days, 30),
    issueDaysBefore: s(pay.issue_days_before_due, 15),
    roundingUnit: String(pay.rounding_unit_cents ?? 1) as PolicyForm['roundingUnit'],
    earlyEnabled: !!pay.early_payment,
    earlyDiscountBp: pay.early_payment?.discount_bp ?? 300,
    earlyPayByDays: s(pay.early_payment?.pay_by_offset_days, 15),
    tacit: p?.renewal?.tacit ?? false,
    noticeDays: s(p?.renewal?.notice_days, 60),
    cancelNoticeDays: s(p?.cancellation?.notice_days, 30),
    refund: p?.cancellation?.refund ?? 'pro_rata',
    penaltyBp: p?.cancellation?.penalty_bp ?? 0,
  };
}

function int(text: string, min: number, max: number): number | null {
  if (!/^\d{1,5}$/.test(text.trim())) return null;
  const n = Number(text.trim());
  return n >= min && n <= max ? n : null;
}

export type PolicyResult =
  { ok: true; policy: ContractPolicy } | { ok: false; field: keyof PolicyForm };

/**
 * Policy JSON from the form, keeping the non-payment keys of the base policy (reprice_on_move, release_policy,
 * allow_segments, fixed dates schedule) untouched.
 */
export function policyFromForm(f: PolicyForm, base: ContractPolicy | undefined): PolicyResult {
  const depositDue = int(f.depositDueDays, 0, 365);
  if (depositDue === null) return { ok: false, field: 'depositDueDays' };
  let deposit: Schemas['PaymentDeposit'] | null = null;
  if (f.depositKind === 'bp') {
    if (f.depositBp === null || f.depositBp < 0 || f.depositBp > 10_000)
      return { ok: false, field: 'depositBp' };
    deposit = { bp: f.depositBp };
  } else if (f.depositKind === 'cents') {
    if (f.depositCents === null || f.depositCents < 0) return { ok: false, field: 'depositCents' };
    deposit = { cents: f.depositCents };
  }
  const issue = int(f.issueDaysBefore, 0, 365);
  if (issue === null) return { ok: false, field: 'issueDaysBefore' };
  let schedule: Schemas['PaymentSchedule'];
  if (f.mode === 'interval') {
    const every = int(f.everyMonths, 1, 12);
    if (every === null) return { ok: false, field: 'everyMonths' };
    const count = int(f.count, 1, 24);
    if (count === null) return { ok: false, field: 'count' };
    const first = int(f.firstOffsetMonths, 0, 12);
    if (first === null) return { ok: false, field: 'firstOffsetMonths' };
    schedule = { mode: 'interval', every_months: every, count, first_due_offset_months: first };
  } else if (f.mode === 'single') {
    const due = int(f.dueOffsetDays, 0, 365);
    if (due === null) return { ok: false, field: 'dueOffsetDays' };
    schedule = { mode: 'single', due_offset_days: due };
  } else {
    const dates = base?.payment?.schedule?.dates ?? [];
    schedule = { mode: 'fixed_dates', dates };
  }
  let early: Schemas['EarlyPayment'] | null = null;
  if (f.earlyEnabled) {
    if (f.earlyDiscountBp === null || f.earlyDiscountBp < 0 || f.earlyDiscountBp > 10_000) {
      return { ok: false, field: 'earlyDiscountBp' };
    }
    const by = int(f.earlyPayByDays, 0, 365);
    if (by === null) return { ok: false, field: 'earlyPayByDays' };
    early = { discount_bp: f.earlyDiscountBp, pay_by_offset_days: by };
  }
  const notice = int(f.noticeDays, 0, 365);
  if (notice === null) return { ok: false, field: 'noticeDays' };
  const cancelNotice = int(f.cancelNoticeDays, 0, 365);
  if (cancelNotice === null) return { ok: false, field: 'cancelNoticeDays' };
  if (f.penaltyBp === null || f.penaltyBp < 0 || f.penaltyBp > 10_000)
    return { ok: false, field: 'penaltyBp' };
  return {
    ok: true,
    policy: {
      ...base,
      version: 2,
      payment: {
        ...base?.payment,
        deposit,
        deposit_due_days: depositDue,
        schedule,
        issue_days_before_due: issue,
        rounding_unit_cents: Number(f.roundingUnit) as 1 | 100 | 1000,
        early_payment: early,
      },
      renewal: { tacit: f.tacit, notice_days: notice },
      cancellation: { notice_days: cancelNotice, refund: f.refund, penalty_bp: f.penaltyBp },
    },
  };
}
