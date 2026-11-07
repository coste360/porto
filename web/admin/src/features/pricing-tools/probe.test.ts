import { describe, expect, it } from 'vitest';

import type { PricingSimulateRequest } from '@porto/shared/api';

import { decodeProbe, encodeProbe } from './probe';
import { probe } from './test/fixtures';

describe('encodeProbe / decodeProbe', () => {
  it('round-trips a full simulate request', () => {
    const req: PricingSimulateRequest = {
      ...probe(1, 1),
      item_id: '0190a000-0000-7000-8000-0000000b1001',
      group_ids: ['g1', 'g2'],
      customer_category: 'affiliated',
      promo_code: 'ESTATE',
      services: [{ item_id: 's1', qty_milli: 1500 }],
      deposit: { percent_bp: 3000 },
      berth_length_cm: 1250,
      berth_width_cm: 450,
    };
    const encoded = encodeProbe(req);
    expect(encoded).toMatch(/^[A-Za-z0-9_-]+$/);
    expect(decodeProbe(encoded)).toEqual(req);
  });

  it('round-trips non-ASCII text', () => {
    const req: PricingSimulateRequest = { ...probe(0, 0), category: 'Pontile-è' };
    expect(decodeProbe(encodeProbe(req))).toEqual(req);
  });

  it.each([
    ['empty', ''],
    ['not base64url', '***'],
    ['not JSON', btoa('hello').replace(/=+$/, '')],
    ['array', btoa('[1,2]').replace(/=+$/, '')],
  ])('refuses %s', (_name, text) => {
    expect(decodeProbe(text)).toBeNull();
  });

  it('refuses unknown contract types, bad dates and floats', () => {
    const enc = (v: unknown) =>
      btoa(JSON.stringify(v)).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
    expect(decodeProbe(enc({ ...probe(0, 0), contract_type: 'weekly' }))).toBeNull();
    expect(decodeProbe(enc({ ...probe(0, 0), arrival: '1/7/2027' }))).toBeNull();
    expect(
      decodeProbe(enc({ ...probe(0, 0), vessel: { loa_cm: 1134.5, beam_cm: 380 } })),
    ).toBeNull();
    expect(decodeProbe(enc(probe(0, 0)))).toEqual(probe(0, 0));
  });
});
