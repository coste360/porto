#!/usr/bin/env node
// Booking bundle budget (spec 04): the gzip size of all JS in dist must stay under 180 KB.
import { readFileSync, readdirSync, statSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { gzipSync } from 'node:zlib';

const LIMIT_BYTES = 180 * 1024;
const dist = join(dirname(fileURLToPath(import.meta.url)), '..', 'dist');

function jsFiles(dir) {
  const out = [];
  for (const name of readdirSync(dir)) {
    const full = join(dir, name);
    if (statSync(full).isDirectory()) out.push(...jsFiles(full));
    else if (name.endsWith('.js')) out.push(full);
  }
  return out;
}

let files;
try {
  files = jsFiles(dist);
} catch {
  console.error('budget: booking/dist not found, run the build first');
  process.exit(1);
}
const total = files.reduce((sum, f) => sum + gzipSync(readFileSync(f), { level: 9 }).length, 0);
const kb = (total / 1024).toFixed(1);
if (total > LIMIT_BYTES) {
  console.error(`budget: booking JS is ${kb} KB gzip, over the ${LIMIT_BYTES / 1024} KB limit`);
  process.exit(1);
}
console.log(`budget: booking JS is ${kb} KB gzip (limit ${LIMIT_BYTES / 1024} KB)`);
