import { describe, expect, it } from 'vitest';

import type { PriceRule } from '@porto/shared/api';

import { newRule } from './model';
import { ruleDefaults, ruleFromForm, ruleSchema } from './ruleSchema';

const bands = ['L10', 'L12'];
const cats = ['L10', 'L12'];

function submit(rule: PriceRule, patch: Partial<ReturnType<typeof ruleDefaults>>) {
  const res = ruleSchema.safeParse({ ...ruleDefaults(rule, bands, cats), ...patch });
  return res;
}

describe('rule form', () => {
  it('a per_meter base rule keeps only unit_cents and its scope', () => {
    const rule = newRule('base', 'Transito');
    const res = submit(rule, {
      item_id: 'i1',
      unit_cents: 750,
      amount_cents: 100,
      scope: {
        contract_types: ['transit'],
        categories: [],
        group_ids: [],
        channels: [],
        customer_categories: [],
        periods: ['ALTA'],
      },
    });
    expect(res.success).toBe(true);
    const out = ruleFromForm(res.data!, rule);
    expect(out).toEqual({
      key: rule.key,
      stage: 'base',
      name: 'Transito',
      active: true,
      priority: 0,
      stackable: false,
      item_id: 'i1',
      basis: 'per_meter',
      unit: 'night',
      unit_cents: 750,
      scope: { contract_types: ['transit'], periods: ['ALTA'] },
    });
  });

  it('a length_band rule maps the grid to amounts and keeps the default amount', () => {
    const rule: PriceRule = {
      id: 'r5',
      stage: 'base',
      name: 'Annuale',
      basis: 'length_band',
      unit: 'year',
      item_id: 'i1',
    };
    const res = submit(rule, {
      amounts: [
        { key: 'L10', cents: 300000 },
        { key: 'L12', cents: null },
      ],
      amount_cents: 400000,
    });
    expect(res.success).toBe(true);
    const out = ruleFromForm(res.data!, rule);
    expect(out.id).toBe('r5');
    expect(out.amounts).toEqual({ L10: 300000 });
    expect(out.amount_cents).toBe(400000);
    expect(out.unit_cents).toBeUndefined();
  });

  it('requires an amount for each basis', () => {
    const rule = newRule('base', 'X');
    expect(submit(rule, { item_id: 'i1', unit_cents: null }).error?.issues[0]?.path).toEqual([
      'unit_cents',
    ]);
    expect(
      submit(rule, { item_id: 'i1', basis: 'flat', amount_cents: null }).error?.issues[0]?.path,
    ).toEqual(['amount_cents']);
    expect(
      submit(rule, { item_id: 'i1', basis: 'per_berth', amounts: [{ key: 'L10', cents: null }] })
        .error?.issues[0]?.message,
    ).toBe('pricing:rules.validation.gridEmpty');
    expect(submit(rule, { item_id: '', unit_cents: 1 }).error?.issues[0]?.path).toEqual([
      'item_id',
    ]);
  });

  it('a promo discount needs a code and keeps it upper case', () => {
    const rule = newRule('discount', 'Promo');
    expect(submit(rule, { discount_kind: 'promo', promo_code: '' }).success).toBe(false);
    const res = submit(rule, {
      discount_kind: 'promo',
      promo_code: 'estate10',
      effect_percent_bp: 1000,
    });
    const out = ruleFromForm(res.data!, rule);
    expect(out).toMatchObject({
      discount_kind: 'promo',
      promo_code: 'ESTATE10',
      effect: { percent_bp: 1000 },
    });
    expect(out.item_id).toBeUndefined();
    expect(out.basis).toBeUndefined();
  });

  it('a customer discount needs a customer category; a discount percent is 0,01..100 %', () => {
    const rule = newRule('discount', 'Soci');
    expect(submit(rule, { discount_kind: 'customer' }).error?.issues[0]?.path).toEqual([
      'scope',
      'customer_categories',
    ]);
    expect(submit(rule, { effect_percent_bp: 0 }).success).toBe(false);
    expect(submit(rule, { effect_percent_bp: 10001 }).success).toBe(false);
  });

  it('a modifier keeps conditions and a signed amount effect', () => {
    const rule = newRule('modifier', 'Multiscafo');
    const res = submit(rule, {
      effect_kind: 'amount',
      effect_amount_cents: -500,
      hull_types: ['catamaran', 'trimaran'],
      min_nights: 3,
    });
    expect(res.success).toBe(true);
    expect(ruleFromForm(res.data!, rule)).toMatchObject({
      stage: 'modifier',
      stackable: true,
      effect: { amount_cents: -500 },
      conditions: { hull_types: ['catamaran', 'trimaran'], min_nights: 3 },
    });
    expect(submit(rule, { min_nights: 5, max_nights: 2 }).success).toBe(false);
  });
});
