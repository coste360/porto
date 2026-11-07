import { describe, expect, it } from 'vitest';

import {
  parseEuroToCents,
  parseMetresToCm,
  parsePercentToBp,
  parseQtyToMilli,
} from '@porto/shared/lib/format';

import { probe } from '../test/fixtures';
import {
  emptySimulatorValues,
  fromSimulateRequest,
  simulatorSchema,
  toSimulateRequest,
  type SimulatorValues,
} from './simulatorValues';

/** Every number of a request is a safe integer. */
function integersOnly(v: unknown): boolean {
  if (typeof v === 'number') return Number.isSafeInteger(v);
  if (Array.isArray(v)) return v.every(integersOnly);
  if (v && typeof v === 'object') return Object.values(v).every(integersOnly);
  return true;
}

const filled = (): SimulatorValues => ({
  ...emptySimulatorValues('2027-03-01'),
  arrival: '2027-07-10',
  departure: '2027-07-13',
  // Typed "11,34" / "3,8" in the length inputs.
  loa_cm: parseMetresToCm('11,34'),
  beam_cm: parseMetresToCm('3,8'),
  category: 'L12',
  customer_category: 'affiliated',
  promo_code: ' estate ',
  services: [{ item_id: 's1', qty_milli: parseQtyToMilli('1,5') }],
  deposit_mode: 'percent',
  deposit_percent_bp: parsePercentToBp('30'),
});

describe('toSimulateRequest', () => {
  it('maps the typed values to integer cents, cm, bp and milli', () => {
    const req = toSimulateRequest(filled());
    expect(req).toEqual({
      contract_type: 'transit',
      arrival: '2027-07-10',
      departure: '2027-07-13',
      channel: 'desk',
      vessel: { loa_cm: 1134, beam_cm: 380, hull_type: 'monohull_motor' },
      category: 'L12',
      customer_category: 'affiliated',
      promo_code: 'ESTATE',
      quoted_on: '2027-03-01',
      services: [{ item_id: 's1', qty_milli: 1500 }],
      deposit: { percent_bp: 3000 },
    });
    expect(integersOnly(req)).toBe(true);
  });

  it('leaves out empty fields so that the server resolves them', () => {
    const v = { ...emptySimulatorValues(), arrival: '2027-07-10', departure: '2027-07-11' };
    expect(toSimulateRequest(v)).toEqual({
      contract_type: 'transit',
      arrival: '2027-07-10',
      departure: '2027-07-11',
      channel: 'desk',
    });
  });

  it('sends a fixed deposit amount in cents and registry ids instead of values', () => {
    const v: SimulatorValues = {
      ...filled(),
      vessel_mode: 'registry',
      vessel_id: 'v1',
      resource_mode: 'berth',
      berth_id: 'b1',
      customer_mode: 'registry',
      customer_id: 'c1',
      deposit_mode: 'amount',
      deposit_amount_cents: parseEuroToCents('100,50'),
    };
    const req = toSimulateRequest(v);
    expect(req.vessel).toBeUndefined();
    expect(req.vessel_id).toBe('v1');
    expect(req.resource).toEqual({ type: 'berth', id: 'b1' });
    expect(req.customer_id).toBe('c1');
    expect(req.customer_category).toBeUndefined();
    expect(req.deposit).toEqual({ amount_cents: 10050 });
  });
});

describe('fromSimulateRequest', () => {
  it('gives back the same request (matrix probe → form → simulate)', () => {
    for (const p of [probe(0, 0), probe(1, 1)]) {
      expect(toSimulateRequest(fromSimulateRequest(p))).toEqual(p);
    }
    const full = toSimulateRequest(filled());
    expect(toSimulateRequest(fromSimulateRequest(full))).toEqual(full);
  });
});

describe('simulatorSchema', () => {
  const messages = (v: SimulatorValues) => {
    const r = simulatorSchema.safeParse(v);
    return r.success ? [] : r.error.issues.map((i) => `${i.path.join('.')}:${i.message}`);
  };

  it('accepts a complete form', () => {
    expect(messages(filled())).toEqual([]);
  });

  it('requires the dates and a departure after the arrival', () => {
    expect(messages(emptySimulatorValues())).toEqual([
      'arrival:common:validation.required',
      'departure:common:validation.required',
    ]);
    expect(messages({ ...filled(), departure: '2027-07-10' })).toEqual([
      'departure:pricingTools:validation.departure',
    ]);
  });

  it('requires both vessel dimensions or none, and the picked ids', () => {
    expect(messages({ ...filled(), beam_cm: null })).toEqual([
      'beam_cm:pricingTools:validation.length',
    ]);
    expect(messages({ ...filled(), vessel_mode: 'registry' })).toEqual([
      'vessel_id:common:validation.required',
    ]);
    expect(messages({ ...filled(), services: [{ item_id: '', qty_milli: 1000 }] })).toEqual([
      'services.0.item_id:common:validation.required',
    ]);
    expect(messages({ ...filled(), deposit_percent_bp: null })).toEqual([
      'deposit_percent_bp:pricingTools:validation.percent',
    ]);
  });
});
