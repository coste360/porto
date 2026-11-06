import { describe, expect, it } from 'vitest';
import { build, checkManifest, sanitizeSvg } from './gen-harbor-kit.mjs';

const wrap = (body) =>
  `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100">${body}</svg>`;

describe('sanitizeSvg', () => {
  it('keeps geometric elements as a tree', () => {
    const nodes = sanitizeSvg(
      wrap(
        '<g fill="none" stroke="currentColor"><rect x="1" y="2" width="3" height="4"/><path d="M0 0 L10 10"/></g>',
      ),
    );
    expect(nodes).toEqual([
      {
        tag: 'g',
        attrs: { fill: 'none', stroke: 'currentColor' },
        children: [
          { tag: 'rect', attrs: { x: '1', y: '2', width: '3', height: '4' }, children: [] },
          { tag: 'path', attrs: { d: 'M0 0 L10 10' }, children: [] },
        ],
      },
    ]);
  });

  it.each([
    ['script', wrap('<script>alert(1)</script>')],
    ['text', wrap('<text x="1">A</text>')],
    ['bare text', wrap('hello<rect x="1"/>')],
    ['use', wrap('<use href="#a"/>')],
    ['image', wrap('<image href="x.png"/>')],
    ['style attribute', wrap('<rect style="fill:red"/>')],
    ['event handler', wrap('<rect onclick="x()"/>')],
    ['hex colour', wrap('<rect fill="#ff0000"/>')],
    ['url paint', wrap('<rect fill="url(a)"/>')],
    ['comment', wrap('<!-- x --><rect x="1"/>')],
    [
      'wrong viewBox',
      '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><rect x="1"/></svg>',
    ],
    ['missing namespace', '<svg viewBox="0 0 100 100"><rect x="1"/></svg>'],
    [
      'unclosed',
      '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><g><rect x="1"/></svg>',
    ],
    ['empty', wrap('')],
  ])('rejects %s', (_name, svg) => {
    expect(() => sanitizeSvg(svg)).toThrow();
  });
});

describe('checkManifest', () => {
  it('rejects duplicates and unknown categories', () => {
    const asset = { id: 'crane', category: 'operations', label_key: 'assets.crane' };
    expect(() =>
      checkManifest({ format: 'porto-harbor-kit/1', id: 'k', assets: [asset, asset] }),
    ).toThrow();
    expect(() =>
      checkManifest({
        format: 'porto-harbor-kit/1',
        id: 'k',
        assets: [{ ...asset, category: 'x' }],
      }),
    ).toThrow();
  });
});

describe('build', () => {
  it('generates the porto-basic kit', () => {
    const out = build();
    expect(out['manifest.ts']).toContain('"harbour_office"');
    expect(out['symbols.ts']).toContain('KIT_SYMBOLS');
    const data = out['symbols.ts'].slice(out['symbols.ts'].indexOf('= {'));
    expect(data).not.toMatch(/<|script|href|style/);
  });
});
