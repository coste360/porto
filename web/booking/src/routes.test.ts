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
});
