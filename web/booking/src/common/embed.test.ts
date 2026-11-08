import { describe, expect, it, vi } from 'vitest';

import {
  actualParentOrigin,
  allowedReturnTo,
  createMessenger,
  embedParams,
  normalizeOrigin,
  verifiedParent,
} from './embed';

const ALLOWED = ['https://marina.example', 'http://localhost:18081'];

describe('embed messaging', () => {
  it('reads ?embed=1&parent= and normalises the parent origin', () => {
    expect(embedParams('?embed=1&parent=https%3A%2F%2Fmarina.example%2Fpath')).toEqual({
      embedded: true,
      parent: 'https://marina.example',
    });
    expect(embedParams('?parent=https%3A%2F%2Fmarina.example')).toEqual({
      embedded: false,
      parent: null,
    });
    expect(embedParams('?embed=1&parent=javascript:alert(1)')).toEqual({
      embedded: true,
      parent: null,
    });
    expect(normalizeOrigin('ftp://x.example')).toBeNull();
  });

  it('prefers ancestorOrigins over the referrer for the real parent', () => {
    const ancestors = { length: 1, 0: 'https://marina.example' } as unknown as DOMStringList;
    expect(
      actualParentOrigin({ referrer: 'https://evil.example/' }, { ancestorOrigins: ancestors }),
    ).toBe('https://marina.example');
    expect(actualParentOrigin({ referrer: 'http://localhost:18081/host.html' }, undefined)).toBe(
      'http://localhost:18081',
    );
    expect(actualParentOrigin({ referrer: '' }, undefined)).toBeNull();
  });

  it('accepts the parent only when allowed and equal to the real parent', () => {
    expect(verifiedParent('https://marina.example', ALLOWED, 'https://marina.example')).toBe(
      'https://marina.example',
    );
    // Claimed origin not in the allowlist.
    expect(verifiedParent('https://evil.example', ALLOWED, 'https://evil.example')).toBeNull();
    // Allowed origin claimed by another embedding page.
    expect(verifiedParent('https://marina.example', ALLOWED, 'https://evil.example')).toBeNull();
    // No referrer information.
    expect(verifiedParent('https://marina.example', ALLOWED, null)).toBeNull();
    expect(
      verifiedParent('https://marina.example', undefined, 'https://marina.example'),
    ).toBeNull();
  });

  it('posts only to the verified origin, never to "*"', () => {
    const target = { postMessage: vi.fn() };
    createMessenger('https://marina.example', target).post({ type: 'porto:step', step: 'details' });
    expect(target.postMessage).toHaveBeenCalledWith(
      { type: 'porto:step', step: 'details' },
      'https://marina.example',
    );
    const silent = { postMessage: vi.fn() };
    createMessenger(null, silent).post({ type: 'porto:booked', reference: 'K7M2Q9XA' });
    expect(silent.postMessage).not.toHaveBeenCalled();
  });

  it('keeps return_to only for allowed origins', () => {
    expect(allowedReturnTo('http://localhost:18081/host.html', ALLOWED)).toBe(
      'http://localhost:18081/host.html',
    );
    expect(allowedReturnTo('https://evil.example/', ALLOWED)).toBeNull();
    expect(allowedReturnTo('javascript:alert(1)', ALLOWED)).toBeNull();
    expect(allowedReturnTo(null, ALLOWED)).toBeNull();
  });
});
