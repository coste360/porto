import { describe, expect, it } from 'vitest';

import type { Charge, ExportPreview } from '@porto/shared/api';

import { offlineReasonOf } from './offline';

import { closeMethods, countedBody, sessionDifference } from './cash';
import { chargeActions, viewFilters } from './charges';
import { canConfirm, formError, previousMonth, toFilter, vatTotalsMatch } from './exportWizard';
import { toMatchRows } from './drafts';
import { divHalfEven, parseQty, previewLine, previewTotals } from './lines';
import {
  allocatedTotal,
  autoFill,
  creditPreview,
  isValidAllocation,
  setRow,
  toMatches,
  type MatchRow,
} from './matching';

const rows: MatchRow[] = [
  { chargeId: 'c3', number: 'A2026-000003', dueOn: '2026-03-01', residualCents: 5_000 },
  { chargeId: 'c1', number: 'A2026-000001', dueOn: '2026-01-01', residualCents: 10_000 },
  { chargeId: 'c2', number: 'A2026-000002', dueOn: '2026-01-01', residualCents: 2_500 },
];

// Deterministic pseudo-random sequence (no Math.random in tests that must be reproducible).
function lcg(seed: number) {
  let s = seed;
  return () => {
    s = (s * 1_103_515_245 + 12_345) % 2_147_483_648;
    return s;
  };
}

describe('matching table', () => {
  it('auto-fills oldest due first, never above a residual, the rest is credit', () => {
    const a = autoFill(rows, 13_000);
    expect(a).toEqual({ c1: 10_000, c2: 2_500, c3: 500 });
    expect(creditPreview(a, 13_000)).toBe(0);
    const b = autoFill(rows, 20_000);
    expect(b).toEqual({ c1: 10_000, c2: 2_500, c3: 5_000 });
    expect(creditPreview(b, 20_000)).toBe(2_500);
    expect(toMatches(rows, a).map((m) => m.charge_id)).toEqual(['c1', 'c2', 'c3']);
  });

  it('clamps a typed row to its residual and to what the payment leaves', () => {
    let a = autoFill(rows, 12_000); // c1 10000, c2 2000
    a = setRow(rows, a, 12_000, 'c3', 9_999);
    expect(a.c3).toBe(0); // nothing left
    a = setRow(rows, a, 12_000, 'c1', 4_000);
    a = setRow(rows, a, 12_000, 'c3', 9_999);
    expect(a.c3).toBe(5_000); // residual cap
    expect(allocatedTotal(a)).toBe(11_000);
    expect(setRow(rows, a, 12_000, 'c2', -5).c2).toBe(0);
  });

  it('property: any sequence of edits keeps rows ≤ residual and Σ ≤ amount', () => {
    const next = lcg(42);
    for (let run = 0; run < 300; run++) {
      const amount = next() % 30_000;
      let a = autoFill(rows, amount);
      for (let step = 0; step < 20; step++) {
        const r = rows[next() % rows.length]!;
        a = setRow(rows, a, amount, r.chargeId, (next() % 25_000) - 2_000);
        expect(isValidAllocation(rows, a, amount)).toBe(true);
        expect(allocatedTotal(a) + creditPreview(a, amount)).toBe(amount);
      }
    }
  });

  it('flags an allocation left invalid after the amount was lowered', () => {
    const a = autoFill(rows, 10_000);
    expect(isValidAllocation(rows, a, 9_000)).toBe(false);
  });
});

describe('close session difference', () => {
  const expected = [
    { method: 'cash', amount_cents: 15_000 },
    { method: 'card_pos', amount_cents: 4_000 },
  ];
  it('shows cash always and the methods with an expected amount', () => {
    expect(closeMethods(expected)).toEqual(['cash', 'card_pos']);
    expect(closeMethods([])).toEqual(['cash']);
  });
  it('computes counted − expected per method and in total', () => {
    const d = sessionDifference(expected, { cash: 14_500, card_pos: 4_000 });
    expect(d.rows.map((r) => r.difference)).toEqual([-500, 0]);
    expect(d.total).toBe(-500);
    expect(d.totalExpected).toBe(19_000);
    expect(countedBody(d)).toEqual([
      { method: 'cash', amount_cents: 14_500 },
      { method: 'card_pos', amount_cents: 4_000 },
    ]);
    expect(sessionDifference(expected, {}).total).toBe(-19_000);
  });
});

describe('line preview', () => {
  it('rounds half-even', () => {
    expect(divHalfEven(25n, 10n)).toBe(2n);
    expect(divHalfEven(35n, 10n)).toBe(4n);
    expect(divHalfEven(-25n, 10n)).toBe(-2n);
    expect(divHalfEven(26n, 10n)).toBe(3n);
  });
  it('splits VAT for gross and net prices', () => {
    expect(
      previewLine({ unitCents: 12_200, qtyMilli: 1000, vatRateBp: 2200, priceIncludesVat: true }),
    ).toEqual({ net: 10_000, vat: 2_200, gross: 12_200 });
    expect(
      previewLine({ unitCents: 10_000, qtyMilli: 1500, vatRateBp: 2200, priceIncludesVat: false }),
    ).toEqual({ net: 15_000, vat: 3_300, gross: 18_300 });
    expect(
      previewLine({
        unitCents: 1_000,
        qtyMilli: 2000,
        discountCents: 500,
        vatRateBp: 1000,
        priceIncludesVat: true,
      }),
    ).toEqual({ net: 1_364, vat: 136, gross: 1_500 });
  });
  it('sums per rate and gross = net + vat', () => {
    const t = previewTotals([
      { unitCents: 12_200, qtyMilli: 1000, vatRateBp: 2200, priceIncludesVat: true },
      null,
      { unitCents: 500, qtyMilli: 3000, vatRateBp: 0, priceIncludesVat: true },
      { unitCents: 1_000, qtyMilli: 1000, vatRateBp: 2200, priceIncludesVat: false },
    ]);
    expect(t.byRate.map((r) => r.vatRateBp)).toEqual([0, 2200]);
    expect(t.gross).toBe(t.net + t.vat);
    expect(t.byRate.reduce((s, r) => s + r.gross, 0)).toBe(t.gross);
  });
  it('parses quantities to milli', () => {
    expect(parseQty('2')).toBe(2000);
    expect(parseQty('1,5')).toBe(1500);
    expect(parseQty('0,125')).toBe(125);
    expect(parseQty('0')).toBeNull();
    expect(parseQty('1,2345')).toBeNull();
    expect(parseQty('abc')).toBeNull();
  });
});

const charge = (over: Partial<Charge> = {}): Charge => ({
  id: 'ch1',
  number: 'A2026-000001',
  origin_type: 'manual',
  origin_id: 'x',
  issued_on: '2026-11-01',
  due_on: '2026-11-30',
  total_cents: 12_200,
  net_cents: 10_000,
  vat_cents: 2_200,
  paid_amount_cents: 0,
  credited_cents: 0,
  residual_cents: 12_200,
  payment_status: 'open',
  invoicing_status: 'to_invoice',
  fiscal_mode: 'invoice',
  description: 'Servizio',
  customer: { id: 'cu1', display_name: 'Rossi' },
  version: 1,
  ...over,
});

describe('charge actions', () => {
  it('offers edit and cancel only on untouched charges, adjust otherwise', () => {
    expect(chargeActions(charge())).toMatchObject({ edit: true, cancel: true, adjust: false });
    const exported = chargeActions(charge({ invoicing_status: 'exported' }));
    expect(exported).toMatchObject({ edit: false, cancel: false, adjust: true, invoiceRef: true });
    const paid = chargeActions(
      charge({ paid_amount_cents: 12_200, residual_cents: 0, payment_status: 'paid' }),
    );
    expect(paid).toMatchObject({ edit: false, adjust: true, collect: false });
    expect(chargeActions(charge({ payment_status: 'cancelled' }))).toMatchObject({
      edit: false,
      adjust: false,
      collect: false,
    });
    expect(chargeActions(charge({ customer: undefined })).requestPayment).toBe(false);
  });
  it('maps saved views to filters', () => {
    expect(viewFilters('toInvoice')).toEqual({ invoicing_status: 'to_invoice' });
    expect(viewFilters('overdue')).toEqual({ overdue: true });
    expect(viewFilters('all')).toEqual({});
  });
});

describe('export wizard', () => {
  it('defaults to the previous month', () => {
    expect(previousMonth('2026-03-15')).toEqual({ from: '2026-02-01', to: '2026-02-28' });
    expect(previousMonth('2026-01-03')).toEqual({ from: '2025-12-01', to: '2025-12-31' });
  });
  it('validates the period and builds the filter body', () => {
    expect(formError({ from: null, to: '2026-01-31', originTypes: [] })).toBe(
      'exports:wizard.errors.period',
    );
    expect(formError({ from: '2026-02-01', to: '2026-01-31', originTypes: [] })).toBe(
      'exports:wizard.errors.order',
    );
    expect(toFilter({ from: '2026-01-01', to: '2026-01-31', originTypes: ['transit'] })).toEqual({
      period_from: '2026-01-01',
      period_to: '2026-01-31',
      origin_types: ['transit'],
    });
  });
  it('confirms only a non-empty preview and checks the VAT totals', () => {
    const p: ExportPreview = {
      count: 2,
      total_cents: 300,
      totals_by_vat: [
        { vat_rate_bp: 2200, net_cents: 164, vat_cents: 36, gross_cents: 200 },
        { vat_rate_bp: 1000, net_cents: 91, vat_cents: 9, gross_cents: 100 },
      ],
      totals_by_origin: [],
      warnings: [],
    };
    expect(canConfirm(p)).toBe(true);
    expect(canConfirm({ ...p, count: 0 })).toBe(false);
    expect(vatTotalsMatch(p)).toBe(true);
    expect(vatTotalsMatch({ ...p, total_cents: 301 })).toBe(false);
  });
});

describe('offline reason of a payment (29.2)', () => {
  const base = {
    kind: 'payment',
    method: 'cash',
    cents: 1000,
    allocated: 1000,
    receiptMode: false,
  };
  it('only fully matched cash on charges not in receipt mode may be queued', () => {
    expect(offlineReasonOf(base)).toBeNull();
    expect(offlineReasonOf({ ...base, receiptMode: true })).toBe('document');
    expect(offlineReasonOf({ ...base, receiptMode: undefined })).toBe('document');
    expect(offlineReasonOf({ ...base, allocated: 600 })).toBe('document');
    expect(offlineReasonOf({ ...base, method: 'card_pos' })).toBe('online_only');
    expect(offlineReasonOf({ ...base, kind: 'deposit' })).toBe('online_only');
  });

  it('marks receipt-mode charges in the matching rows', () => {
    const charge = (id: string, fiscal_mode: string) =>
      ({
        id,
        number: id,
        due_on: '2026-01-01',
        residual_cents: 100,
        payment_status: 'open',
        fiscal_mode,
      }) as unknown as Charge;
    expect(
      toMatchRows([charge('a', 'receipt'), charge('b', 'invoice')]).map((r) => r.receiptMode),
    ).toEqual([true, false]);
  });
});
