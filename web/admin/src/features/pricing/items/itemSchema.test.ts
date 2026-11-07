import { describe, expect, it } from 'vitest';

import { makeItem, settings } from '../test/fixtures';
import { itemCreateBody, itemDefaults, itemPatchBody, itemSchema } from './itemSchema';

describe('item form', () => {
  it('defaults come from the pricing settings', () => {
    const d = itemDefaults(null, {
      ...settings,
      default_vat_rate_bp: 1000,
      default_price_includes_vat: true,
    });
    expect(d.vat_preset).toBe('1000');
    expect(d.price_includes_vat).toBe(true);
    expect(itemDefaults(null, { ...settings, default_vat_rate_bp: 850 })).toMatchObject({
      vat_preset: 'custom',
      vat_custom_bp: 850,
    });
  });

  it('a 0 % rate requires the nature; a non-zero rate never sends it', () => {
    const base = {
      ...itemDefaults(null, settings),
      code: 'srv-x',
      name_it: 'Servizio',
      revenue_class_id: 'rc1',
    };
    const zero = itemSchema.safeParse({ ...base, vat_preset: '0' });
    expect(zero.success).toBe(false);
    expect(zero.error?.issues[0]?.path).toEqual(['vat_nature']);
    const ok = itemSchema.safeParse({ ...base, vat_preset: '0', vat_nature: 'N4' });
    expect(itemCreateBody(ok.data!)).toMatchObject({
      code: 'SRV-X',
      vat_rate_bp: 0,
      vat_nature: 'N4',
    });
    const taxed = itemSchema.safeParse({ ...base, vat_preset: '2200', vat_nature: 'N4' });
    expect(itemCreateBody(taxed.data!)).not.toHaveProperty('vat_nature');
  });

  it('a custom rate must be a valid percentage', () => {
    const base = {
      ...itemDefaults(null, settings),
      code: 'A',
      name_it: 'A',
      revenue_class_id: 'rc1',
    };
    expect(
      itemSchema.safeParse({ ...base, vat_preset: 'custom', vat_custom_bp: null }).success,
    ).toBe(false);
    expect(
      itemSchema.safeParse({ ...base, vat_preset: 'custom', vat_custom_bp: 10001 }).success,
    ).toBe(false);
    const ok = itemSchema.safeParse({ ...base, vat_preset: 'custom', vat_custom_bp: 850 });
    expect(itemCreateBody(ok.data!).vat_rate_bp).toBe(850);
  });

  it('rejects codes outside the pattern', () => {
    const base = { ...itemDefaults(null, settings), name_it: 'A', revenue_class_id: 'rc1' };
    expect(itemSchema.safeParse({ ...base, code: 'ORM ANN' }).success).toBe(false);
    expect(itemSchema.safeParse({ ...base, code: '-ORM' }).success).toBe(false);
  });

  it('the patch carries only changed fields and clears the nature with a non-zero rate', () => {
    const item = makeItem({ vat_rate_bp: 0, vat_nature: 'N4' });
    const v = itemSchema.parse({
      ...itemDefaults(item, settings),
      vat_preset: '2200',
      vat_nature: '',
    });
    expect(itemPatchBody(v, item)).toEqual({ vat_rate_bp: 2200, vat_nature: null });
    expect(itemPatchBody(itemSchema.parse(itemDefaults(item, settings)), item)).toEqual({});
  });
});
