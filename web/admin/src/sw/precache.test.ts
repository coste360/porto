import { PLACEHOLDER, buildId, inject, precacheList } from '../../scripts/sw-precache.mjs';

const manifest = {
  'index.html': {
    file: 'assets/index-B1.js',
    css: ['assets/index-C2.css'],
    assets: ['assets/titillium-D3.woff2'],
  },
  '_chunk-E4.js': { file: 'assets/chunk-E4.js' },
  'src/sw/sw.ts': { file: 'sw.js' },
  'src/pwa/register.ts': { file: 'assets/register-F5.js' },
};

describe('sw-precache', () => {
  it('lists the shell deterministically: sorted, deduplicated, without the worker, maps and the manifest dir', () => {
    const pub = [
      'site.webmanifest',
      'favicon.ico',
      'android-chrome-192x192.png',
      'robots.txt',
      'x.js.map',
    ];
    const a = precacheList(manifest, pub);
    const b = precacheList(
      Object.fromEntries(Object.entries(manifest).reverse()),
      [...pub].reverse(),
    );
    expect(a).toEqual(b);
    expect(a).toEqual([
      '/admin/android-chrome-192x192.png',
      '/admin/assets/chunk-E4.js',
      '/admin/assets/index-B1.js',
      '/admin/assets/index-C2.css',
      '/admin/assets/register-F5.js',
      '/admin/assets/titillium-D3.woff2',
      '/admin/favicon.ico',
      '/admin/index.html',
      '/admin/site.webmanifest',
    ]);
    expect(buildId(a)).toBe(buildId(b));
    expect(buildId(a)).toMatch(/^[0-9a-f]{16}$/);
    expect(buildId([...a, '/admin/assets/new-G6.js'])).not.toBe(buildId(a));
  });

  it('injects the list into the worker', () => {
    const src = `const p = ${PLACEHOLDER} ?? null;`;
    const out = inject(src, { version: '1.2.3', build: 'abc', urls: ['/admin/index.html'] });
    expect(out).toBe(
      'const p = ({"version":"1.2.3","build":"abc","urls":["/admin/index.html"]}) ?? null;',
    );
    expect(() => inject('nothing here', { version: '', build: '', urls: [] })).toThrow();
  });
});
