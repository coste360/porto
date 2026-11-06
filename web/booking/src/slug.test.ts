import { describe, expect, it } from 'vitest';

import { slugFromPath } from './slug';

describe('slugFromPath', () => {
  it('reads the marina slug', () => {
    expect(slugFromPath('/b/prova')).toBe('prova');
    expect(slugFromPath('/b/marina-di-prova/')).toBe('marina-di-prova');
  });
  it('rejects anything else', () => {
    expect(slugFromPath('/b/')).toBeNull();
    expect(slugFromPath('/b/Prova')).toBeNull();
    expect(slugFromPath('/admin/x')).toBeNull();
  });
});
