#!/usr/bin/env node
// Builds assets/brand/porto/outlined/: the standard Porto logotypes with the "porto" wordmark
// converted to paths from Titillium Web 700 (SIL OFL 1.1) instead of Inter Bold (ADR 0007, D13).
//
// Inputs:
//   09_Sorgenti_Modificabili/porto_logo_{horizontal,stacked}_editable.svg
//     -> wordmark geometry: x, y, font-size, letter-spacing, text-anchor.
//   01_Loghi_SVG/porto_logo_{horizontal,stacked}_*.svg
//     -> one output per standard variant; header, background, symbol and colours are kept
//        byte for byte, only the outlined Inter wordmark group is replaced.
//
// The output is deterministic: fixed font file, fixed rounding, files written in sorted order.
// Usage: npm --prefix scripts/brand ci && npm --prefix scripts/brand run build   (or `make brand`)

import { readFileSync, readdirSync, mkdirSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';

import opentype from 'opentype.js';

const here = dirname(fileURLToPath(import.meta.url));
const kit = join(here, '..', '..', 'assets', 'brand', 'porto');
const standardDir = join(kit, '01_Loghi_SVG');
const editableDir = join(kit, '09_Sorgenti_Modificabili');
const outDir = join(kit, 'outlined');

const require = createRequire(import.meta.url);
const fontFile = require.resolve(
  '@fontsource/titillium-web/files/titillium-web-latin-700-normal.woff',
);
const font = opentype.loadSync(fontFile);

const LAYOUTS = ['horizontal', 'stacked'];

/** Formats a number with at most `digits` decimals and no trailing zeros ("-0" becomes "0"). */
function num(value, digits = 3) {
  const rounded = Number(value.toFixed(digits));
  return Object.is(rounded, -0) ? '0' : String(rounded);
}

/** Reads the attributes of the single <text> wordmark of an editable source. */
function readWordmark(svg, file) {
  const match = svg.match(/<text\b([^>]*)>([^<]*)<\/text>/);
  if (!match) throw new Error(`${file}: no <text> wordmark`);
  const attrs = Object.fromEntries(
    [...match[1].matchAll(/([a-z-]+)="([^"]*)"/g)].map(([, k, v]) => [k, v]),
  );
  if (attrs['font-weight'] !== '700' || !/^Titillium Web\b/.test(attrs['font-family'] ?? '')) {
    throw new Error(`${file}: wordmark is not set in Titillium Web 700`);
  }
  return {
    text: match[2],
    x: Number(attrs.x),
    y: Number(attrs.y),
    size: Number(attrs['font-size']),
    letterSpacing: Number(attrs['letter-spacing'] ?? 0),
    anchor: attrs['text-anchor'] ?? 'start',
  };
}

/** Returns the symbol paths of an SVG with their fills removed, to compare geometry only. */
function symbolGeometry(svg) {
  const group = svg.match(/<g transform="translate\([^)]*\)(?: scale\(\.94\))?">(.*?)<\/g>/);
  return group ? group[1].replace(/ fill="[^"]*"/g, '') : null;
}

/** One glyph path in font units (y up), with the pen offset baked into a translate. */
function glyphPath(glyph, penX) {
  const d = glyph.path.commands
    .map((c) => {
      switch (c.type) {
        case 'M':
        case 'L':
          return `${c.type}${num(c.x)} ${num(c.y)}`;
        case 'Q':
          return `Q${num(c.x1)} ${num(c.y1)} ${num(c.x)} ${num(c.y)}`;
        case 'C':
          return `C${num(c.x1)} ${num(c.y1)} ${num(c.x2)} ${num(c.y2)} ${num(c.x)} ${num(c.y)}`;
        case 'Z':
          return 'Z';
        default:
          throw new Error(`unexpected path command ${c.type}`);
      }
    })
    .join('');
  return d ? `<path transform="translate(${num(penX)} 0)" d="${d}"/>` : '';
}

/**
 * Lays the wordmark out as the browser does for SVG text: advance + kerning per glyph, plus
 * letter-spacing after every character (the last one included, which matters for
 * text-anchor="middle"). Returns the outlined group for the given fill.
 */
function outlineWordmark(mark, fill) {
  const scale = mark.size / font.unitsPerEm;
  const spacing = mark.letterSpacing / scale; // font units
  const glyphs = font.stringToGlyphs(mark.text);
  const pens = [];
  let pen = 0;
  glyphs.forEach((glyph, i) => {
    pens.push(pen);
    const kern = i + 1 < glyphs.length ? font.getKerningValue(glyph, glyphs[i + 1]) : 0;
    pen += glyph.advanceWidth + kern + spacing;
  });
  const width = pen * scale;
  const startX =
    mark.anchor === 'middle' ? mark.x - width / 2 : mark.anchor === 'end' ? mark.x - width : mark.x;
  const paths = glyphs.map((g, i) => glyphPath(g, pens[i])).join('');
  const s = num(scale, 7);
  return {
    svg: `<g transform="translate(${num(startX)} ${num(mark.y)}) scale(${s} -${s})" fill="${fill}">${paths}</g>`,
    bounds: inkBounds(glyphs, pens, scale, startX, mark.y),
  };
}

/** Ink bounding box of the outlined wordmark in user units, to check it fits the viewBox. */
function inkBounds(glyphs, pens, scale, x0, y0) {
  let x1 = Infinity, y1 = Infinity, x2 = -Infinity, y2 = -Infinity;
  glyphs.forEach((g, i) => {
    const b = g.getBoundingBox();
    x1 = Math.min(x1, x0 + (pens[i] + b.x1) * scale);
    x2 = Math.max(x2, x0 + (pens[i] + b.x2) * scale);
    y1 = Math.min(y1, y0 - b.y2 * scale);
    y2 = Math.max(y2, y0 - b.y1 * scale);
  });
  return { x1, y1, x2, y2 };
}

const INTER_WORDMARK =
  /<g transform="translate\([^)]*\) scale\([^)]*\)" fill="(#[0-9a-fA-F]{3,8})">(?:<path [^>]*\/>)+<\/g>/;

mkdirSync(outDir, { recursive: true });
const written = [];

for (const layout of LAYOUTS) {
  const editableName = `porto_logo_${layout}_editable.svg`;
  const editable = readFileSync(join(editableDir, editableName), 'utf8');
  const mark = readWordmark(editable, editableName);
  const geometry = symbolGeometry(editable);

  const variants = readdirSync(standardDir)
    .filter((f) => f.startsWith(`porto_logo_${layout}_`) && f.endsWith('.svg'))
    .sort();

  for (const name of variants) {
    const standard = readFileSync(join(standardDir, name), 'utf8');
    if (symbolGeometry(standard) !== geometry) {
      throw new Error(`${name}: symbol differs from ${editableName}`);
    }
    const found = standard.match(INTER_WORDMARK);
    if (!found) throw new Error(`${name}: outlined wordmark group not found`);

    const { svg: wordmark, bounds } = outlineWordmark(mark, found[1]);
    const [, vbW, vbH] = standard.match(/viewBox="0 0 ([\d.]+) ([\d.]+)"/).map(Number);
    if (bounds.x1 < 0 || bounds.y1 < 0 || bounds.x2 > vbW || bounds.y2 > vbH) {
      throw new Error(`${name}: wordmark ink ${JSON.stringify(bounds)} exceeds viewBox`);
    }

    writeFileSync(join(outDir, name), standard.replace(INTER_WORDMARK, wordmark));
    written.push(`${name}  ink x ${num(bounds.x1, 1)}..${num(bounds.x2, 1)} y ${num(bounds.y1, 1)}..${num(bounds.y2, 1)}`);
  }
}

console.log(`Titillium Web 700 from ${fontFile.split('node_modules/')[1]}`);
for (const line of written) console.log(`outlined/${line}`);
