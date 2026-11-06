#!/usr/bin/env node
// Harbour illustration kit generator (spec 7.7, m1-plan §2.7), run by `npm --prefix web run gen`.
//
// Reads assets/harbor-kits/porto-basic/{manifest.json,svg/<id>.svg}, checks that the manifest and the files
// match, and sanitises every SVG with an allowlist: only geometric elements, geometric and presentation
// attributes, `fill`/`stroke` limited to `none` or `currentColor`, no text, no styles, no scripts, no links.
// Anything else is an error. The output is a JSON element tree that a small React renderer turns into
// <symbol> elements, so markup is never injected as HTML:
//   web/shared/src/map/kit/generated/manifest.ts
//   web/shared/src/map/kit/generated/symbols.ts
// Node built-ins only.
import { mkdirSync, readFileSync, readdirSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

export const WEB_ROOT = join(dirname(fileURLToPath(import.meta.url)), '..');
export const KIT_DIR = join(WEB_ROOT, '..', 'assets', 'harbor-kits', 'porto-basic');
export const OUT_DIR = join(WEB_ROOT, 'shared', 'src', 'map', 'kit', 'generated');

export const ALLOWED_ELEMENTS = new Set([
  'g',
  'path',
  'rect',
  'circle',
  'ellipse',
  'line',
  'polyline',
  'polygon',
]);
export const ALLOWED_ATTRIBUTES = new Set([
  'd',
  'x',
  'y',
  'width',
  'height',
  'rx',
  'ry',
  'cx',
  'cy',
  'r',
  'x1',
  'y1',
  'x2',
  'y2',
  'points',
  'transform',
  'fill',
  'stroke',
  'stroke-width',
  'stroke-linecap',
  'stroke-linejoin',
  'stroke-miterlimit',
  'stroke-dasharray',
  'fill-rule',
  'clip-rule',
  'opacity',
  'fill-opacity',
  'stroke-opacity',
]);
const PAINT_ATTRIBUTES = new Set(['fill', 'stroke']);
const PAINT_VALUES = new Set(['none', 'currentColor']);
const ID_PATTERN = /^[a-z][a-z0-9_]{0,63}$/;
const CATEGORIES = new Set(['services', 'operations', 'navigation', 'safety', 'environment']);
// Attribute values: numbers, path data, point lists, simple transforms and keywords. No url(), quotes, `;` or `&`.
const VALUE_PATTERN = /^[A-Za-z0-9 .,\-()#]*$/;

/** Splits an attribute string into name/value pairs; throws on anything that is not name="value". */
function parseAttributes(src, where) {
  const attrs = {};
  const re = /\s*([A-Za-z_:][A-Za-z0-9_:.-]*)\s*=\s*"([^"<>]*)"\s*/y;
  let pos = 0;
  while (pos < src.length) {
    re.lastIndex = pos;
    const m = re.exec(src);
    if (!m) {
      if (src.slice(pos).trim() === '') break;
      throw new Error(`${where}: malformed attributes near "${src.slice(pos, pos + 20)}"`);
    }
    if (m[1] in attrs) throw new Error(`${where}: duplicate attribute ${m[1]}`);
    attrs[m[1]] = m[2];
    pos = re.lastIndex;
  }
  return attrs;
}

function checkAttributes(tag, attrs, where) {
  for (const [name, value] of Object.entries(attrs)) {
    if (!ALLOWED_ATTRIBUTES.has(name))
      throw new Error(`${where}: attribute "${name}" not allowed on <${tag}>`);
    if (/^on/i.test(name)) throw new Error(`${where}: event handler "${name}"`);
    if (!VALUE_PATTERN.test(value) || /url\s*\(/i.test(value) || value.includes('#')) {
      throw new Error(`${where}: value of "${name}" not allowed: "${value}"`);
    }
    if (PAINT_ATTRIBUTES.has(name) && !PAINT_VALUES.has(value)) {
      throw new Error(`${where}: ${name}="${value}" (only none or currentColor)`);
    }
  }
}

/**
 * Parses and sanitises one kit SVG. Returns the list of child nodes of the root <svg>
 * ({ tag, attrs, children }). Throws with a message naming the offending construct.
 */
export function sanitizeSvg(text, where = 'svg') {
  const src = text.trim();
  if (/<!--|<\?|<!\[CDATA\[|<!DOCTYPE/i.test(src)) {
    throw new Error(
      `${where}: comments, processing instructions, CDATA and DOCTYPE are not allowed`,
    );
  }
  const tagRe = /<\s*(\/?)\s*([A-Za-z][A-Za-z0-9:-]*)([^<>]*?)(\/?)\s*>/g;
  const stack = [];
  let root = null;
  let last = 0;
  for (let m = tagRe.exec(src); m; m = tagRe.exec(src)) {
    const between = src.slice(last, m.index);
    if (between.trim() !== '')
      throw new Error(`${where}: text content is not allowed ("${between.trim().slice(0, 20)}")`);
    last = tagRe.lastIndex;
    const [, closing, tag, rawAttrs, selfClosing] = m;
    if (closing) {
      const open = stack.pop();
      if (!open || open.tag !== tag) throw new Error(`${where}: unexpected </${tag}>`);
      continue;
    }
    const attrs = parseAttributes(rawAttrs, `${where} <${tag}>`);
    if (!root) {
      if (tag !== 'svg') throw new Error(`${where}: root element must be <svg>`);
      const { xmlns, viewBox, ...rest } = attrs;
      if (xmlns !== 'http://www.w3.org/2000/svg')
        throw new Error(`${where}: missing SVG namespace`);
      if (viewBox !== '0 0 100 100') throw new Error(`${where}: viewBox must be "0 0 100 100"`);
      if (Object.keys(rest).length > 0)
        throw new Error(
          `${where}: attributes not allowed on <svg>: ${Object.keys(rest).join(', ')}`,
        );
      root = { tag: 'svg', attrs: {}, children: [] };
      if (!selfClosing) stack.push(root);
      continue;
    }
    if (stack.length === 0) throw new Error(`${where}: content after </svg>`);
    if (!ALLOWED_ELEMENTS.has(tag)) throw new Error(`${where}: element <${tag}> not allowed`);
    checkAttributes(tag, attrs, `${where} <${tag}>`);
    const node = { tag, attrs, children: [] };
    stack[stack.length - 1].children.push(node);
    if (!selfClosing) stack.push(node);
  }
  if (src.slice(last).trim() !== '') throw new Error(`${where}: trailing content`);
  if (!root) throw new Error(`${where}: no <svg> element`);
  if (stack.length > 0) throw new Error(`${where}: unclosed <${stack[stack.length - 1].tag}>`);
  if (root.children.length === 0) throw new Error(`${where}: empty symbol`);
  return root.children;
}

/** Checks the manifest object and returns its assets. */
export function checkManifest(manifest) {
  if (
    manifest?.format !== 'porto-harbor-kit/1' ||
    typeof manifest.id !== 'string' ||
    !Array.isArray(manifest.assets)
  ) {
    throw new Error('manifest: unexpected format');
  }
  const seen = new Set();
  for (const a of manifest.assets) {
    if (!ID_PATTERN.test(a?.id ?? '') || seen.has(a.id))
      throw new Error(`manifest: invalid or duplicate id "${a?.id}"`);
    if (!CATEGORIES.has(a.category))
      throw new Error(`manifest: ${a.id}: unknown category "${a.category}"`);
    if (typeof a.label_key !== 'string' || a.label_key === '')
      throw new Error(`manifest: ${a.id}: missing label_key`);
    seen.add(a.id);
  }
  return manifest.assets;
}

/** Builds the generated sources from a kit directory. Returns { 'manifest.ts': string, 'symbols.ts': string }. */
export function build(kitDir = KIT_DIR) {
  const manifest = JSON.parse(readFileSync(join(kitDir, 'manifest.json'), 'utf8'));
  const assets = checkManifest(manifest);
  const files = readdirSync(join(kitDir, 'svg')).filter((f) => f.endsWith('.svg'));
  const ids = new Set(assets.map((a) => a.id));
  for (const f of files) {
    if (!ids.has(f.slice(0, -4))) throw new Error(`svg/${f}: not in the manifest`);
  }
  const symbols = {};
  for (const a of assets) {
    if (!files.includes(`${a.id}.svg`))
      throw new Error(`manifest: ${a.id}: svg/${a.id}.svg missing`);
    symbols[a.id] = sanitizeSvg(
      readFileSync(join(kitDir, 'svg', `${a.id}.svg`), 'utf8'),
      `svg/${a.id}.svg`,
    );
  }
  const header =
    '// Code generated by web/scripts/gen-harbor-kit.mjs from assets/harbor-kits; DO NOT EDIT.\n';
  const idUnion = assets.map((a) => JSON.stringify(a.id)).join(' | ');
  const manifestTs =
    header +
    `\nexport const KIT_ID = ${JSON.stringify(manifest.id)};\n\n` +
    `export type KitAssetId = ${idUnion};\n\n` +
    `export type KitCategory = 'services' | 'operations' | 'navigation' | 'safety' | 'environment';\n\n` +
    `export interface KitAsset {\n  id: KitAssetId;\n  category: KitCategory;\n  labelKey: string;\n}\n\n` +
    `export const KIT_ASSETS: readonly KitAsset[] = ${JSON.stringify(
      assets.map((a) => ({ id: a.id, category: a.category, labelKey: a.label_key })),
      null,
      2,
    )};\n`;
  const symbolsTs =
    header +
    `\nimport type { KitAssetId } from './manifest';\n\n` +
    `export interface KitNode {\n  tag: 'g' | 'path' | 'rect' | 'circle' | 'ellipse' | 'line' | 'polyline' | 'polygon';\n` +
    `  attrs: Readonly<Record<string, string>>;\n  children: readonly KitNode[];\n}\n\n` +
    `export const KIT_SYMBOLS: Readonly<Record<KitAssetId, readonly KitNode[]>> = ${JSON.stringify(symbols, null, 2)};\n`;
  return { 'manifest.ts': manifestTs, 'symbols.ts': symbolsTs };
}

function main() {
  const out = build();
  mkdirSync(OUT_DIR, { recursive: true });
  for (const [name, content] of Object.entries(out)) writeFileSync(join(OUT_DIR, name), content);
  console.log(
    `gen-harbor-kit: ${Object.keys(out).length} files written to shared/src/map/kit/generated`,
  );
}

if (import.meta.url === pathToFileURL(process.argv[1] ?? '').href) {
  try {
    main();
  } catch (err) {
    console.error(`gen-harbor-kit: ${err.message}`);
    process.exit(1);
  }
}
