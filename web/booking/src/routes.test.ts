import { describe, expect, it } from 'vitest';

import { routeFromPath } from './routes';
import { slugFromPath } from './slug';

describe('booking routes', () => {
  it('routes customer links by purpose', () => {
    expect(routeFromPath('/b/porto-demo/m/signature/ABCDEFGHIJKLMNOP1234', slugFromPath)).toEqual({
      page: 'link',
      slug: 'porto-demo',
      purpose: 'signature',
      token: 'ABCDEFGHIJKLMNOP1234',
    });
    expect(routeFromPath('/b/porto-demo/m/other/ABCDEFGHIJKLMNOP1234', slugFromPath)).toEqual({
      page: 'marina',
      slug: null,
    });
    expect(routeFromPath('/b/porto-demo', slugFromPath)).toEqual({
      page: 'marina',
      slug: 'porto-demo',
    });
  });

  it('routes the payment page and its return page', () => {
    expect(routeFromPath('/b/porto-demo/pay/ABCDEFGHIJKLMNOP1234', slugFromPath)).toEqual({
      page: 'pay',
      slug: 'porto-demo',
      token: 'ABCDEFGHIJKLMNOP1234',
      ret: false,
    });
    expect(routeFromPath('/b/porto-demo/pay/ABCDEFGHIJKLMNOP1234/return', slugFromPath)).toEqual({
      page: 'pay',
      slug: 'porto-demo',
      token: 'ABCDEFGHIJKLMNOP1234',
      ret: true,
    });
  });

  it('routes the M8 return page, customer area and login link', () => {
    expect(
      routeFromPath('/b/porto-demo/r/0192f0c4-1111-7000-8000-000000000001', slugFromPath),
    ).toEqual({
      page: 'return',
      slug: 'porto-demo',
      bookingId: '0192f0c4-1111-7000-8000-000000000001',
    });
    expect(routeFromPath('/b/porto-demo/account', slugFromPath)).toEqual({
      page: 'account',
      slug: 'porto-demo',
      contractId: null,
    });
    expect(
      routeFromPath('/b/porto-demo/account/0192f0c4-1111-7000-8000-000000000002', slugFromPath),
    ).toEqual({
      page: 'account',
      slug: 'porto-demo',
      contractId: '0192f0c4-1111-7000-8000-000000000002',
    });
    expect(routeFromPath('/b/porto-demo/a', slugFromPath)).toEqual({
      page: 'accountLink',
      slug: 'porto-demo',
    });
  });

  it('routes the M12 public marina profile', () => {
    expect(routeFromPath('/p/porto-demo', slugFromPath)).toEqual({
      page: 'profile',
      slug: 'porto-demo',
    });
    expect(routeFromPath('/p/porto-demo/', slugFromPath)).toEqual({
      page: 'profile',
      slug: 'porto-demo',
    });
    expect(routeFromPath('/p/Porto_Demo', slugFromPath)).toEqual({ page: 'marina', slug: null });
    expect(routeFromPath('/p/porto-demo/extra', slugFromPath)).toEqual({
      page: 'marina',
      slug: null,
    });
    expect(routeFromPath('/p/ab', slugFromPath)).toEqual({ page: 'marina', slug: null });
  });
});
