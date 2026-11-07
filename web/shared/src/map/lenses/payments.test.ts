import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

import { describe, expect, it } from 'vitest';

import { daySubjects } from '../dayView';
import {
  PAYMENTS_LEGEND,
  TOKEN_CLASSES,
  applyPaymentsLens,
  hasPaymentWarning,
  paymentsStyle,
  indexMapPayments,
} from './index';
import { DATE, makeDayView } from './testFixtures';

const dv = makeDayView();
const subjects = daySubjects(dv);
const berths = subjects.filter((s) => s.resourceType === 'berth' && s.ref === s.resourceId);

const res = (i: number, state: 'ok' | 'due_soon' | 'overdue' | 'overdue_60' | 'disputed') => ({
  resource_type: 'berth' as const,
  resource_id: berths[i]!.resourceId,
  contract_id: `c${i}`,
  state,
  suspended: false,
});

const status = {
  date: DATE,
  resources: [
    res(0, 'ok'),
    res(1, 'due_soon'),
    { ...res(2, 'overdue'), residual_cents: 12_345 },
    res(3, 'overdue_60'),
    res(4, 'disputed'),
    { ...res(5, 'ok'), suspended: true },
    { ...res(0, 'overdue'), resource_id: 'not-on-this-map' },
  ],
};

describe('payments lens (Pagamenti)', () => {
  it('maps every state to a token, glyph and legend key and counts every subject once', () => {
    const r = applyPaymentsLens(subjects, status);
    expect(r.styles.get(berths[0]!.ref)).toMatchObject({ colorToken: 'ok', paymentsKey: 'ok' });
    expect(r.styles.get(berths[1]!.ref)).toMatchObject({
      colorToken: 'warn',
      glyph: 'clock',
      paymentsKey: 'due_soon',
    });
    expect(r.styles.get(berths[2]!.ref)).toMatchObject({
      colorToken: 'bad',
      glyph: 'alert',
      badge: '€',
      paymentsKey: 'overdue',
      contractId: 'c2',
      residualCents: 12_345,
    });
    expect(r.styles.get(berths[3]!.ref)).toMatchObject({ badge: '€€', paymentsKey: 'overdue_60' });
    expect(r.styles.get(berths[4]!.ref)).toMatchObject({ paymentsKey: 'disputed' });
    expect(r.styles.get(berths[5]!.ref)).toMatchObject({
      colorToken: 'blocked',
      glyph: 'lock',
      paymentsKey: 'suspended',
    });
    const total = Object.values(r.counts).reduce((a, b) => a + b, 0);
    expect(total).toBe(subjects.length);
    expect(r.counts.none).toBe(subjects.length - 6);
  });

  it('never conveys a problem state by colour alone', () => {
    const r = applyPaymentsLens(subjects, status);
    for (const s of r.styles.values()) {
      if (s.paymentsKey !== 'ok' && s.paymentsKey !== 'none') expect(s.glyph).toBeDefined();
    }
  });

  it('treats a missing answer as no data and dims hidden keys', () => {
    const r = applyPaymentsLens(subjects, undefined, new Set(['none']));
    expect(r.counts.none).toBe(subjects.length);
    expect(r.dimmed.size).toBe(subjects.length);
  });

  it('matches on resource type as well as id', () => {
    const index = indexMapPayments(status);
    const s = { ...berths[2]!, resourceType: 'land_resource' as const };
    expect(paymentsStyle(s, index).paymentsKey).toBe('none');
  });

  it('warns the dock on overdue, disputed or suspended only', () => {
    expect(hasPaymentWarning({ state: 'ok', suspended: false })).toBe(false);
    expect(hasPaymentWarning({ state: 'due_soon', suspended: false })).toBe(false);
    expect(hasPaymentWarning({ state: 'overdue', suspended: false })).toBe(true);
    expect(hasPaymentWarning({ state: 'disputed', suspended: false })).toBe(true);
    expect(hasPaymentWarning({ state: 'ok', suspended: true })).toBe(true);
  });

  it('uses only known tokens and has a label for every legend key in it and en', () => {
    const r = applyPaymentsLens(subjects, status);
    for (const s of r.styles.values()) expect(TOKEN_CLASSES[s.colorToken]).toBeDefined();
    const dir = join(dirname(fileURLToPath(import.meta.url)), '..', '..', 'i18n', 'locales');
    for (const lng of ['it', 'en']) {
      const enums = JSON.parse(readFileSync(join(dir, lng, 'billingEnums.json'), 'utf8')) as {
        lensState: Record<string, string>;
      };
      const rec = JSON.parse(readFileSync(join(dir, lng, 'receivables.json'), 'utf8')) as {
        lens: { none: string };
      };
      for (const k of PAYMENTS_LEGEND)
        expect(k === 'none' ? rec.lens.none : enums.lensState[k]).toBeTruthy();
    }
  });
});
