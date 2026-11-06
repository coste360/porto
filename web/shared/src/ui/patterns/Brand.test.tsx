import { render } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import { BRAND_FILES, BrandLogo } from './Brand';

// Vite may inline the SVGs as data URIs, so sources are compared with the imports.
const srcs = (container: HTMLElement) =>
  Array.from(container.querySelectorAll('img')).map((img) => img.getAttribute('src') ?? '');

describe('BrandLogo', () => {
  it('auto renders the light and dark files, switched by the theme class', () => {
    const { container } = render(<BrandLogo />);
    const imgs = container.querySelectorAll('img');
    expect(srcs(container)).toEqual([BRAND_FILES.horizontal.light, BRAND_FILES.horizontal.dark]);
    expect(imgs[0]?.className).toContain('dark:hidden');
    expect(imgs[1]?.className).toContain('dark:block');
  });

  it.each([
    ['onLight', BRAND_FILES.horizontal.light],
    ['onDark', BRAND_FILES.horizontal.dark],
    ['onNavy', BRAND_FILES.horizontal.navy],
  ] as const)('tone %s uses its file', (tone, file) => {
    const { container } = render(<BrandLogo tone={tone} />);
    expect(srcs(container)).toEqual([file]);
  });

  it('the files are the official ones', () => {
    // Inlined data URIs keep the original file's title; URLs keep the file name.
    const check = (src: string, name: string, title: string) =>
      expect(src.includes(name) || decodeURIComponent(src).includes(title)).toBe(true);
    check(BRAND_FILES.horizontal.light, 'porto_logo_horizontal_primary', 'logo orizzontale');
    check(BRAND_FILES.symbol.light, 'porto_symbol_primary', 'simbolo');
    expect(BRAND_FILES.horizontal.navy).not.toBe(BRAND_FILES.horizontal.dark);
  });

  it('symbol variant uses the symbol files', () => {
    const { container } = render(<BrandLogo variant="symbol" tone="onLight" />);
    expect(srcs(container)).toEqual([BRAND_FILES.symbol.light]);
  });

  it('keeps the minimum sizes and the alt text', () => {
    const { container, getAllByAltText } = render(<BrandLogo size="sm" tone="onLight" />);
    expect(container.querySelector('img')?.className).toContain('min-w-30');
    expect(getAllByAltText('Porto')).toHaveLength(1);
    const symbol = render(<BrandLogo variant="symbol" size="sm" tone="onLight" />);
    expect(symbol.container.querySelector('img')?.className).toContain('size-6');
  });
});
