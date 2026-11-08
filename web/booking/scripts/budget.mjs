#!/usr/bin/env node
// Booking bundle budget (spec 04, m8-plan §12.1, §16 D19), measured on Vite's build manifest:
//   - first load: the entry chunk plus every chunk it imports statically (recursively), gzip ≤ 180 KB;
//   - guard: all JS of the build, gzip ≤ 450 KB.
// Lazy chunks (dynamicImports) do not count toward the first load.
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { gzipSync } from 'node:zlib';

export const ENTRY_LIMIT_BYTES = 180 * 1024;
export const TOTAL_LIMIT_BYTES = 450 * 1024;

/** Files of the entry chunks and of their static imports (JS only), in a stable order. */
export function firstLoadFiles(manifest) {
  const seen = new Set();
  const files = [];
  const visit = (key) => {
    if (seen.has(key)) return;
    seen.add(key);
    const chunk = manifest[key];
    if (!chunk) return;
    if (chunk.file && chunk.file.endsWith('.js')) files.push(chunk.file);
    for (const imp of chunk.imports ?? []) visit(imp);
  };
  for (const [key, chunk] of Object.entries(manifest)) {
    if (chunk.isEntry) visit(key);
  }
  return files;
}

/** Every JS file named by the manifest. */
export function allJsFiles(manifest) {
  return [
    ...new Set(
      Object.values(manifest)
        .map((c) => c.file)
        .filter((f) => f && f.endsWith('.js')),
    ),
  ];
}

/** Measures a manifest with a gzip size function; returns sizes in bytes and the verdict. */
export function measure(manifest, gzSize) {
  const sum = (files) => files.reduce((n, f) => n + gzSize(f), 0);
  const entry = sum(firstLoadFiles(manifest));
  const total = sum(allJsFiles(manifest));
  const errors = [];
  if (entry > ENTRY_LIMIT_BYTES) errors.push('entry');
  if (total > TOTAL_LIMIT_BYTES) errors.push('total');
  return { entry, total, ok: errors.length === 0, errors };
}

const kb = (n) => (n / 1024).toFixed(1);

function main() {
  const dist = join(dirname(fileURLToPath(import.meta.url)), '..', 'dist');
  let manifest;
  try {
    manifest = JSON.parse(readFileSync(join(dist, '.vite', 'manifest.json'), 'utf8'));
  } catch {
    console.error('budget: booking/dist/.vite/manifest.json not found, run the build first');
    process.exit(1);
  }
  const r = measure(manifest, (f) => gzipSync(readFileSync(join(dist, f)), { level: 9 }).length);
  const line =
    `budget: booking first load ${kb(r.entry)} KB gzip (limit ${kb(ENTRY_LIMIT_BYTES)} KB), ` +
    `all JS ${kb(r.total)} KB gzip (guard ${kb(TOTAL_LIMIT_BYTES)} KB)`;
  if (!r.ok) {
    console.error(`${line}: over the limit (${r.errors.join(', ')})`);
    process.exit(1);
  }
  console.log(line);
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) main();
