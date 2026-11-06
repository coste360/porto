import { readFileSync } from 'node:fs';
import { join } from 'node:path';

import { describe, expect, it } from 'vitest';

import { contrastRatio } from './contrast';
import { parseBlock } from './cssTokens';

const css = readFileSync(join(__dirname, '..', 'styles', 'globals.css'), 'utf8');

/** Foreground/background pairs that carry text. */
const TEXT_PAIRS: [string, string][] = [
  ['foreground', 'background'],
  ['card-foreground', 'card'],
  ['popover-foreground', 'popover'],
  ['primary-foreground', 'primary'],
  ['secondary-foreground', 'secondary'],
  ['muted-foreground', 'muted'],
  ['muted-foreground', 'background'],
  ['accent-foreground', 'accent'],
  ['destructive', 'background'],
  ['destructive-foreground', 'destructive'],
  ['link', 'background'],
  ['sidebar-foreground', 'sidebar'],
  ['sidebar-primary', 'sidebar'],
  ['sidebar-primary-foreground', 'sidebar-primary'],
  ['sidebar-accent-foreground', 'sidebar-accent'],
];

describe.each([':root', '.dark'] as const)('contrast in %s', (selector) => {
  const vars = { ...parseBlock(css, ':root'), ...parseBlock(css, selector) };
  const color = (name: string) => {
    const value = vars[name];
    if (!value) throw new Error(`missing --${name}`);
    return value;
  };

  it.each(TEXT_PAIRS)('%s on %s is at least 4.5:1', (fg, bg) => {
    expect(contrastRatio(color(fg), color(bg))).toBeGreaterThanOrEqual(4.5);
  });

  it('focus ring is at least 3:1 on the background', () => {
    expect(contrastRatio(color('ring'), color('background'))).toBeGreaterThanOrEqual(3);
  });
});

describe('contrastRatio', () => {
  it('matches the WCAG reference values', () => {
    expect(contrastRatio('#000000', '#ffffff')).toBeCloseTo(21, 5);
    expect(contrastRatio('#ffffff', '#ffffff')).toBeCloseTo(1, 5);
  });

  it('confirms sky never carries text on white', () => {
    const vars = parseBlock(css, ':root');
    expect(contrastRatio(vars['brand-sky'] ?? '', vars['brand-white'] ?? '')).toBeLessThan(3);
  });
});
