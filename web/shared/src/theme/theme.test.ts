import { readFileSync } from 'node:fs';
import { join } from 'node:path';

import { describe, expect, it } from 'vitest';

import { parseBlock } from './cssTokens';

const repo = join(__dirname, '..', '..', '..', '..');
const css = readFileSync(join(__dirname, '..', 'styles', 'globals.css'), 'utf8');
const tokens = JSON.parse(
  readFileSync(join(repo, 'assets/brand/porto/08_Developer_Tokens/porto-tokens.json'), 'utf8'),
) as { colors: Record<'navy' | 'sky' | 'black' | 'white', string> };

describe('theme tokens', () => {
  const root = parseBlock(css, ':root');
  const dark = parseBlock(css, '.dark');

  it('brand variables equal the official design tokens', () => {
    expect(root['brand-navy']).toBe(tokens.colors.navy);
    expect(root['brand-sky']).toBe(tokens.colors.sky);
    expect(root['brand-black']).toBe(tokens.colors.black);
    expect(root['brand-white']).toBe(tokens.colors.white);
  });

  it('primary is the brand navy in light mode', () => {
    expect(root.primary).toBe(tokens.colors.navy);
    // The light theme has a light sidebar (acba753); its primary stays the brand navy.
    expect(root['sidebar-primary']).toBe(tokens.colors.navy);
    expect(dark.primary).toBe(tokens.colors.sky);
  });

  it('dark mode redefines every colour token of light mode', () => {
    const colourKeys = Object.keys(root).filter((k) => !k.startsWith('brand-') && k !== 'radius');
    for (const key of colourKeys) expect(dark, key).toHaveProperty(key);
  });

  it('uses Titillium Web as the UI font', () => {
    expect(css).toMatch(/--font-sans:\s*'Titillium Web'/);
  });
});
