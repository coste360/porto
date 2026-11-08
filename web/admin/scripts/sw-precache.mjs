#!/usr/bin/env node
// Post-build step of the admin panel (m6b-plan §3.1): reads dist/.vite/manifest.json, lists the shell (index.html,
// every emitted JS/CSS/font/image, the web manifest and the icons) and replaces the self.__PORTO_PRECACHE__
// placeholder in dist/sw.js with {version, build, urls}. `build` is the SHA-256 of the sorted list, so a new
// build gets a new cache and an unchanged one keeps it. No dependency.
import { createHash } from 'node:crypto';
import { existsSync, readFileSync, readdirSync, writeFileSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

export const PLACEHOLDER = 'self.__PORTO_PRECACHE__';
const BASE = '/admin/';
const PUBLIC_FILES = /\.(png|svg|ico|webmanifest)$/;
const NEVER = new Set(['sw.js']);

/** Files of the Vite manifest (entries, imports, CSS, assets), relative to dist. */
export function manifestFiles(manifest) {
  const out = new Set();
  for (const chunk of Object.values(manifest)) {
    if (chunk.file) out.add(chunk.file);
    for (const f of chunk.css ?? []) out.add(f);
    for (const f of chunk.assets ?? []) out.add(f);
  }
  return out;
}

/** The sorted, deduplicated precache list (absolute URLs under /admin/). Pure. */
export function precacheList(manifest, publicFiles = []) {
  const files = manifestFiles(manifest);
  for (const f of publicFiles) if (PUBLIC_FILES.test(f)) files.add(f);
  files.add('index.html');
  return [...files]
    .filter((f) => !NEVER.has(f) && !f.endsWith('.map') && !f.startsWith('.vite/'))
    .map((f) => BASE + f.replace(/^\/+/, ''))
    .sort();
}

/** Build id of a list (SHA-256 hex, first 16 chars). Pure. */
export function buildId(urls) {
  return createHash('sha256').update(urls.join('\n')).digest('hex').slice(0, 16);
}

/** Replaces the placeholder in the worker source. */
export function inject(source, precache) {
  if (!source.includes(PLACEHOLDER)) throw new Error('sw-precache: placeholder not found in sw.js');
  return source.split(PLACEHOLDER).join(`(${JSON.stringify(precache)})`);
}

export function run(dist, version) {
  const manifest = JSON.parse(readFileSync(join(dist, '.vite', 'manifest.json'), 'utf8'));
  const publicFiles = readdirSync(dist).filter((f) => PUBLIC_FILES.test(f));
  const urls = precacheList(manifest, publicFiles);
  const swPath = join(dist, 'sw.js');
  if (!existsSync(swPath)) throw new Error('sw-precache: dist/sw.js missing');
  const precache = { version, build: buildId(urls), urls };
  writeFileSync(swPath, inject(readFileSync(swPath, 'utf8'), precache));
  return precache;
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const dist = resolve(fileURLToPath(new URL('..', import.meta.url)), 'dist');
  const p = run(dist, process.env.PORTO_VERSION || process.env.VITE_PORTO_VERSION || 'dev');
  console.log(`sw-precache: ${p.urls.length} files, build ${p.build}`);
}
