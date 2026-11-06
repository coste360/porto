import { describe, expect, it } from 'vitest';

import { checkParties, type PartyRow } from './parties';

const row = (over: Partial<PartyRow>): PartyRow => ({
  key: over.key ?? 'k',
  customer_id: 'c1',
  display_name: 'Cliente',
  role: 'owner',
  share: '100',
  ...over,
});

describe('checkParties', () => {
  it('accepts 600 + 400 owners and a billing company', () => {
    const r = checkParties([
      row({ key: 'a', share: '60' }),
      row({ key: 'b', customer_id: 'c2', share: '40' }),
      row({ key: 'c', customer_id: 'c3', role: 'billing', share: '' }),
    ]);
    expect(r).toEqual({
      ok: true,
      ownerTotal: 1000,
      parties: [
        { customer_id: 'c1', role: 'owner', share_millis: 600 },
        { customer_id: 'c2', role: 'owner', share_millis: 400 },
        { customer_id: 'c3', role: 'billing' },
      ],
    });
  });

  it('refuses owner shares above 100 %', () => {
    const r = checkParties([
      row({ key: 'a', share: '60' }),
      row({ key: 'b', customer_id: 'c2', share: '40' }),
      row({ key: 'c', customer_id: 'c3', share: '10' }),
    ]);
    expect(r.ok).toBe(false);
    expect(r.ownerTotal).toBe(1100);
    if (!r.ok) expect(r.errors.total).toBe('vessels:parties.validation.sharesExceed');
  });

  it('needs a customer, an owner share and unique customer-role pairs', () => {
    const r = checkParties([
      row({ key: 'a', customer_id: '' }),
      row({ key: 'b', share: '' }),
      row({ key: 'c', customer_id: 'c2', share: '1,25' }),
      row({ key: 'd', customer_id: 'c3', role: 'contact', share: '' }),
      row({ key: 'e', customer_id: 'c3', role: 'contact', share: '' }),
    ]);
    expect(r.ok).toBe(false);
    if (!r.ok) {
      expect(r.errors).toEqual({
        a: 'vessels:parties.validation.customerRequired',
        b: 'vessels:parties.validation.shareRequired',
        c: 'vessels:parties.validation.shareInvalid',
        e: 'vessels:parties.validation.duplicate',
      });
    }
  });
});
