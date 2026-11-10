#!/usr/bin/env node
// Converts the product screenshots of docs/screenshots/*.png (the same images
// as the README) into the WebP files used by the documentation,
// src/assets/screenshots/<name>.webp. Run it after a screenshot in
// docs/screenshots/ is added or replaced (`make docs-screenshots`); the output
// is committed, so `make docs-build` never needs this step.
//
// sharp comes with Astro (image service), no extra dependency.
import { mkdirSync, readdirSync } from 'node:fs';
import { basename, dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import sharp from 'sharp';

const docsRoot = join(dirname(fileURLToPath(import.meta.url)), '..');
const from = join(docsRoot, '..', 'docs', 'screenshots');
const to = join(docsRoot, 'src', 'assets', 'screenshots');
const only = (process.env.DOCS_ONLY ?? '')
  .split(',')
  .map((s) => s.trim())
  .filter(Boolean);

mkdirSync(to, { recursive: true });
const files = readdirSync(from)
  .filter((f) => f.endsWith('.png'))
  .filter((f) => only.length === 0 || only.includes(basename(f, '.png')))
  .sort();
if (files.length === 0) {
  console.error('screenshots: no matching PNG in docs/screenshots/');
  process.exit(1);
}
for (const file of files) {
  const out = join(to, `${basename(file, '.png')}.webp`);
  const info = await sharp(join(from, file)).webp({ quality: 82, effort: 6 }).toFile(out);
  console.log(`screenshots: ${file} -> src/assets/screenshots/${basename(out)} (${Math.round(info.size / 1024)} KB)`);
}
