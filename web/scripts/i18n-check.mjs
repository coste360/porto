#!/usr/bin/env node
// i18n check, run by `npm run lint`:
//   - it (source) and en have the same key set in every namespace;
//   - no empty values;
//   - every literal key `'<ns>:<key>'` used in admin/src, booking/src and shared/src exists (a plural base key
//     such as `'ns:pending'` exists when `pending_one`/`pending_other` do).
import { readFileSync, readdirSync, statSync } from 'node:fs';
import { dirname, join, relative, sep } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

export const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..');
const LOCALES_DIR = 'shared/src/i18n/locales';
export const LOCALES = ['it', 'en'];
const SOURCE_DIRS = ['admin/src', 'booking/src', 'shared/src'];
const SKIP_DIRS = new Set(['node_modules', 'dist', 'generated', 'locales']);

/** Flattens nested JSON into dotted keys. */
export function flatten(obj, prefix = '', out = new Map()) {
  for (const [k, v] of Object.entries(obj)) {
    const key = prefix ? `${prefix}.${k}` : k;
    if (v && typeof v === 'object' && !Array.isArray(v)) flatten(v, key, out);
    else out.set(key, v);
  }
  return out;
}

/** Compares locales: `{ [locale]: { [ns]: object } }`. Returns error strings. */
export function compareLocales(byLocale, source = 'it') {
  const errors = [];
  const sourceNs = Object.keys(byLocale[source] ?? {});
  for (const [locale, namespaces] of Object.entries(byLocale)) {
    const nsSet = new Set([...sourceNs, ...Object.keys(namespaces)]);
    for (const ns of nsSet) {
      const a = flatten(byLocale[source]?.[ns] ?? {});
      const b = flatten(namespaces[ns] ?? {});
      for (const [key, value] of b) {
        if (typeof value !== 'string' || value.trim() === '') {
          errors.push(`${locale}/${ns}.json: empty or non-string value for "${key}"`);
        }
        if (locale !== source && !a.has(key)) {
          errors.push(`${locale}/${ns}.json: "${key}" is not in ${source}`);
        }
      }
      if (locale !== source) {
        for (const key of a.keys()) {
          if (!b.has(key)) errors.push(`${locale}/${ns}.json: missing "${key}"`);
        }
      }
    }
  }
  return errors;
}

/** Finds `'ns:key'` literals (quotes or backticks without interpolation). */
export function findKeyLiterals(text, namespaces) {
  const ns = namespaces.map((n) => n.replace(/[^\w-]/g, '')).join('|');
  const re = new RegExp(`['"\`](${ns}):([A-Za-z0-9_.-]+)['"\`]`, 'g');
  const found = [];
  let m;
  while ((m = re.exec(text)) !== null) found.push({ ns: m[1], key: m[2] });
  return found;
}

/** i18next plural suffixes (CLDR categories); `t('ns:key', { count })` resolves `key_<category>`. */
export const PLURAL_SUFFIXES = ['zero', 'one', 'two', 'few', 'many', 'other'];

/** True when `key` exists, or is the base of plural keys (`key_one`, `key_other`, ...). */
export function hasKey(keys, key) {
  return keys.has(key) || PLURAL_SUFFIXES.some((s) => keys.has(`${key}_${s}`));
}

/** Returns errors for literal keys missing from the source locale. */
export function checkUsages(files, sourceLocale) {
  const namespaces = Object.keys(sourceLocale);
  const flat = Object.fromEntries(namespaces.map((n) => [n, flatten(sourceLocale[n])]));
  const errors = [];
  for (const [path, text] of files) {
    for (const { ns, key } of findKeyLiterals(text, namespaces)) {
      const keys = flat[ns];
      // A key may also be an object prefix used with returnObjects: not used in M0.
      if (!hasKey(keys, key)) errors.push(`${path}: unknown i18n key "${ns}:${key}"`);
    }
  }
  return errors;
}

function walk(dir, out) {
  let entries;
  try {
    entries = readdirSync(dir);
  } catch {
    return out;
  }
  for (const name of entries) {
    if (SKIP_DIRS.has(name)) continue;
    const full = join(dir, name);
    if (statSync(full).isDirectory()) walk(full, out);
    else if (/\.(ts|tsx)$/.test(name)) out.push(full);
  }
  return out;
}

export function loadLocales(root = ROOT) {
  const byLocale = {};
  for (const locale of LOCALES) {
    byLocale[locale] = {};
    const dir = join(root, LOCALES_DIR, locale);
    for (const name of readdirSync(dir)) {
      if (!name.endsWith('.json')) continue;
      byLocale[locale][name.replace(/\.json$/, '')] = JSON.parse(
        readFileSync(join(dir, name), 'utf8'),
      );
    }
  }
  return byLocale;
}

export function run(root = ROOT) {
  const byLocale = loadLocales(root);
  const errors = compareLocales(byLocale, 'it');
  const files = [];
  for (const dir of SOURCE_DIRS) {
    for (const file of walk(join(root, dir), [])) {
      files.push([relative(root, file).split(sep).join('/'), readFileSync(file, 'utf8')]);
    }
  }
  errors.push(...checkUsages(files, byLocale.it));
  return errors;
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const errors = run();
  if (errors.length > 0) {
    console.error('i18n-check failed:');
    for (const e of errors) console.error(`  ${e}`);
    process.exit(1);
  }
  console.log('i18n-check: ok');
}
