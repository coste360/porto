import { describe, expect, it } from 'vitest';

import { THEME_FILE, checkSource, run } from './css-guard.mjs';

const rules = (path, text) => checkSource(path, text).map((v) => v.rule);

describe('css-guard', () => {
  it('allows only the theme stylesheet', () => {
    expect(rules(THEME_FILE, 'a{}')).toEqual([]);
    expect(rules('admin/src/app.css', 'a{}')).toEqual(['stylesheet']);
    expect(rules('admin/src/x.module.scss', 'a{}')).toEqual(['stylesheet']);
  });

  it('rejects inline styles and programmatic styling', () => {
    expect(rules('admin/src/A.tsx', '<div style={{ color: 1 }} />')).toContain('inline-style');
    expect(rules('admin/src/a.ts', 'el.style.color = x;')).toContain('style-property');
    expect(rules('admin/src/a.ts', "el.style['color'] = x;")).toContain('style-property');
    expect(rules('admin/src/a.ts', 'el.setProperty("--x", "1")')).toContain('css-text');
    expect(rules('admin/src/A.tsx', '<style>{x}</style>')).toContain('style-tag');
  });

  it('rejects colour literals in TS', () => {
    expect(rules('admin/src/a.ts', "const c = '#0b2542';")).toContain('hex-colour');
    expect(rules('admin/src/a.ts', "const c = 'rgb(1, 2, 3)';")).toContain('colour-function');
    expect(rules('admin/src/a.ts', "navigate('/reset#token=abc')")).toEqual([]);
    expect(rules('shared/src/theme/contrast.test.ts', "ratio('#000000', '#ffffff')")).toEqual([]);
  });

  it('rejects arbitrary Tailwind values but allows variants and selectors', () => {
    expect(rules('admin/src/A.tsx', '<p className="w-[13px]" />')).toContain('arbitrary-value');
    expect(rules('admin/src/A.tsx', '<p className="p-2 bg-[#fff]" />')).toContain(
      'arbitrary-value',
    );
    expect(rules('admin/src/A.tsx', '<p className="[--x:1]" />')).toContain('arbitrary-value');
    expect(rules('admin/src/A.tsx', '<p className="data-[state=open]:bg-accent" />')).toEqual([]);
    expect(rules('admin/src/A.tsx', '<p className="[&>svg]:size-4 text-sm" />')).toEqual([]);
  });

  it('rejects CSS imports other than the theme', () => {
    expect(rules('admin/src/main.tsx', "import '@porto/shared/styles/globals.css';")).toEqual([]);
    expect(rules('admin/src/main.tsx', "import './app.css';")).toContain('css-import');
  });

  it('leaves shadcn CLI output alone but still checks its imports', () => {
    expect(
      rules('shared/src/ui/sidebar.tsx', '<div style={{ x: 1 }} className="w-[3px]" />'),
    ).toEqual([]);
    expect(rules('shared/src/ui/patterns/X.tsx', '<div style={{ x: 1 }} />')).toContain(
      'inline-style',
    );
  });

  it('rejects styles in html', () => {
    expect(rules('admin/index.html', '<style>a{}</style>')).toContain('html-style-tag');
    expect(rules('admin/index.html', '<div style="x"></div>')).toContain('html-style-attr');
  });

  it('passes on the repository', () => {
    expect(run()).toEqual([]);
  });
});
