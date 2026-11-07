#!/usr/bin/env node
// Contrast check of the operational map lens tokens (M3, m3-plan §8.2), run by `npm run lint`:
// every --harbor-* colour must reach 3:1 against the water fill of the map (the chart-2 token at 15 % over the
// background, see shared/src/map/tokens.ts) in the light and in the dark theme.
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..');
const CSS = join(ROOT, 'shared/src/styles/globals.css');
const WATER_ALPHA = 0.15;
const MIN_RATIO = 3;

/** Returns the custom properties of the first block opened by selector. */
export function block(css, selector) {
  const start = css.indexOf(`${selector} {`);
  if (start < 0) throw new Error(`no ${selector} block`);
  const body = css.slice(start, css.indexOf('}', start));
  const vars = {};
  for (const m of body.matchAll(/--([\w-]+):\s*(#[0-9a-fA-F]{6})\s*;/g)) vars[m[1]] = m[2];
  return vars;
}

function rgb(hex) {
  const n = parseInt(hex.slice(1), 16);
  return [(n >> 16) & 255, (n >> 8) & 255, n & 255];
}

function luminance([r, g, b]) {
  const c = [r, g, b].map((v) => {
    const s = v / 255;
    return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
  });
  return 0.2126 * c[0] + 0.7152 * c[1] + 0.0722 * c[2];
}

/** WCAG contrast ratio of two RGB triples. */
export function ratio(a, b) {
  const [la, lb] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (la + 0.05) / (lb + 0.05);
}

/** Errors for the harbor tokens of one theme. */
export function check(theme, vars) {
  const bg = rgb(vars.background);
  const tint = rgb(vars['chart-2']);
  const water = bg.map((v, i) => Math.round(v * (1 - WATER_ALPHA) + tint[i] * WATER_ALPHA));
  const errors = [];
  const names = Object.keys(vars).filter((k) => k.startsWith('harbor-'));
  if (names.length === 0) errors.push(`${theme}: no --harbor-* tokens`);
  for (const k of names) {
    const r = ratio(rgb(vars[k]), water);
    if (r < MIN_RATIO)
      errors.push(
        `${theme}: --${k} ${vars[k]} has contrast ${r.toFixed(2)} < ${MIN_RATIO} on water`,
      );
  }
  return errors;
}

if (import.meta.url === `file://${process.argv[1]}`) {
  const css = readFileSync(CSS, 'utf8');
  const light = block(css, ':root');
  const dark = { ...light, ...block(css, '.dark') };
  const errors = [...check('light', light), ...check('dark', dark)];
  if (errors.length > 0) {
    for (const e of errors) console.error(`contrast-check: ${e}`);
    process.exit(1);
  }
  console.log('contrast-check: harbor tokens ok');
}
