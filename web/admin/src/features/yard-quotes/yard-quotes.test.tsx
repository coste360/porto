import { screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import type { YardQuote, YardQuoteLine } from '@porto/shared/api';

import { MARINA_ID, makeMe, renderApp } from '../../test/renderApp';
import { API, baseRoutes } from '../desk/test/fixtures';
import { canAdvance, initialWizard, isManual, toQuoteInput, valueText, wizardReducer } from './lib';

const QUOTE_ID = '0190a000-0000-7000-8000-0000000ee001';

const line = (seq: number, over: Partial<YardQuoteLine> = {}): YardQuoteLine => ({
  seq,
  source: 'package',
  item_id: `i${seq}`,
  item_code: `ITEM-${seq}`,
  work_type_id: 'wt',
  description: `Riga ${seq}`,
  qty_milli: 12000,
  unit_cents: 1800,
  discount_cents: 0,
  qty_overridden: false,
  price_overridden: false,
  manual: false,
  net_cents: 21600,
  vat_cents: 4752,
  gross_cents: 26352,
  vat_rate_bp: 2200,
  price_includes_vat: false,
  estimated_hours_milli: 0,
  issues: [],
  ...over,
});

const quote = (over: Partial<YardQuote> = {}): YardQuote => ({
  id: QUOTE_ID,
  number: 'PC2026-000012',
  kind: 'job',
  customer_id: 'c',
  vessel_id: 'v',
  status: 'draft',
  quoted_on: '2026-11-07',
  valid_until: '2026-12-07',
  expires_at: '2026-12-07T23:00:00Z',
  billing_mode: 'actual',
  conditions: '',
  notes: '',
  lines: [line(1), line(2, { qty_overridden: true, manual: true, description: 'Lavaggio' })],
  totals: {
    net_cents: 170260,
    vat_cents: 37457,
    gross_cents: 207717,
    deposit_amount_cents: 62315,
    vat: [{ vat_rate_bp: 2200, net_cents: 170260, vat_cents: 37457, gross_cents: 207717 }],
  },
  estimated_hours: [{ qualification_id: 'q1', hours_milli: 8400 }],
  issues: [],
  blocking: false,
  language_version: 1,
  version: 2,
  created_at: '2026-11-07T10:00:00Z',
  updated_at: '2026-11-07T10:00:00Z',
  ...over,
});

describe('yard quote wizard state', () => {
  it('advances only when the step is complete', () => {
    let s = initialWizard();
    expect(canAdvance(s)).toBe(false);
    s = wizardReducer(s, { type: 'next' });
    expect(s.step).toBe('vessel');
    s = wizardReducer(s, { type: 'customer', id: 'c' });
    s = wizardReducer(s, { type: 'vessel', id: 'v' });
    s = wizardReducer(s, { type: 'next' });
    expect(s.step).toBe('catalog');
    s = wizardReducer(s, { type: 'next' });
    expect(s.step).toBe('catalog');
    s = wizardReducer(s, { type: 'toggle', selection: { package_id: 'p' } });
    s = wizardReducer(s, { type: 'next' });
    expect(s.step).toBe('variables');
    s = wizardReducer(s, { type: 'back' });
    expect(s.step).toBe('catalog');
  });

  it('marks overrides and drops them when the selection changes', () => {
    let s = initialWizard();
    s = wizardReducer(s, { type: 'toggle', selection: { package_id: 'p' } });
    s = wizardReducer(s, { type: 'override', seq: 2, field: 'qty_milli', value: 60000 });
    s = wizardReducer(s, { type: 'override', seq: 2, field: 'discount_cents', value: 500 });
    expect(s.overrides).toEqual([{ seq: 2, qty_milli: 60000, discount_cents: 500 }]);
    s = wizardReducer(s, { type: 'override', seq: 2, field: 'discount_cents', value: null });
    expect(s.overrides).toEqual([{ seq: 2, qty_milli: 60000 }]);
    s = wizardReducer(s, { type: 'resetLine', seq: 2 });
    expect(s.overrides).toEqual([]);
    s = wizardReducer(s, { type: 'override', seq: 1, field: 'unit_cents', value: 1000 });
    s = wizardReducer(s, { type: 'toggle', selection: { item_id: 'i' } });
    expect(s.overrides).toEqual([]);
    expect(s.selections).toEqual([{ package_id: 'p' }, { item_id: 'i' }]);
    s = wizardReducer(s, { type: 'toggle', selection: { package_id: 'p' } });
    expect(s.selections).toEqual([{ item_id: 'i' }]);
  });

  it('builds the request without prices or quantities unless overridden', () => {
    let s = initialWizard();
    s = wizardReducer(s, { type: 'customer', id: 'c' });
    s = wizardReducer(s, { type: 'vessel', id: 'v' });
    s = wizardReducer(s, { type: 'toggle', selection: { package_id: 'p' } });
    expect(toQuoteInput(s)).toEqual({
      customer_id: 'c',
      vessel_id: 'v',
      selections: [{ package_id: 'p' }],
      billing_mode: 'actual',
    });
    s = wizardReducer(s, { type: 'jobInput', name: 'layers', value: 3000 });
    s = wizardReducer(s, { type: 'terms', patch: { depositBp: 2000, conditions: '  Saldo  ' } });
    expect(toQuoteInput(s)).toMatchObject({
      job_inputs: { layers: 3000 },
      deposit_bp: 2000,
      conditions: 'Saldo',
    });
  });

  it('formats values and the manual badge', () => {
    expect(valueText(2500, 'it')).toBe('2,5');
    expect(valueText('catamaran')).toBe('catamaran');
    expect(valueText(undefined)).toBe('');
    expect(isManual(line(1))).toBe(false);
    expect(isManual(line(1, { price_overridden: true }))).toBe(true);
  });
});

describe('yard quote page', () => {
  it('shows the lines with the manual badge, the totals and the hours', async () => {
    renderApp(`/m/${MARINA_ID}/yard/quotes/${QUOTE_ID}`, [
      ...baseRoutes(makeMe({}, ['marina.read', 'yard.read', 'yard.manage'])),
      { path: `${API}/yard/quotes/${QUOTE_ID}`, body: quote() },
      {
        path: `${API}/yard/qualifications`,
        body: {
          items: [
            {
              id: 'q1',
              code: 'hand',
              name: { it: 'Operaio' },
              labour_item_id: 'l',
              default_cost_cents: 2200,
              lift_operator: false,
              active: true,
              sort_order: 0,
              version: 1,
              created_at: '',
              updated_at: '',
            },
          ],
        },
      },
    ]);
    await screen.findByTestId('yard-quote-page');
    const rows = screen.getAllByTestId('yard-quote-line');
    expect(rows).toHaveLength(2);
    expect(screen.getAllByTestId('manual-badge')).toHaveLength(1);
    expect(screen.getByTestId('total-gross')).toHaveTextContent('2.077,17');
    expect(screen.getByTestId('total-deposit')).toHaveTextContent('623,15');
    expect(screen.getByTestId('estimated-hours')).toHaveTextContent('Operaio: 8,4 ore stimate');
    expect(screen.getByRole('button', { name: 'Ricalcola' })).toBeInTheDocument();
  });
});
