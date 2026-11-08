#!/usr/bin/env node
// Licence check of the production dependencies of every workspace (`npm run licenses`).
// license-checker does not follow dependencies hoisted by npm workspaces, so this reads
// the installed tree with `npm query .prod` instead. An optional directory argument checks another npm project
// (`make licenses` passes scripts/brand, the dev-only brand build).
import { execFileSync } from 'node:child_process';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..');

/**
 * Allowed SPDX ids (docs/m0-plan.md §0.7, §7.1): licences compatible with BSL 1.1 and AGPL-3.0, the same policy as
 * Arenile. 0BSD, CC0-1.0 and BlueOak-1.0.0 are permissive and approved. OFL-1.1 only for the font package.
 */
export const ALLOWED = new Set([
  'MIT',
  'BSD-2-Clause',
  'BSD-3-Clause',
  'Apache-2.0',
  'ISC',
  'MPL-2.0',
  '0BSD',
  'CC0-1.0',
  'BlueOak-1.0.0',
]);
export const FONT_ONLY = { 'OFL-1.1': ['@fontsource/titillium-web'] };

/**
 * Per-package exceptions, `name: reason`. Each one needs the maintainers' approval.
 * isbot: approved (Unlicense, public domain dedication), same as in Arenile.
 */
export const EXCEPTIONS = new Map([
  ['isbot', 'Unlicense (public domain dedication); transitive dep of @tanstack/react-router'],
]);

function spdx(license) {
  if (typeof license === 'string') return license;
  if (license && typeof license.type === 'string') return license.type;
  return 'UNKNOWN';
}

/**
 * Splits simple SPDX expressions without nesting: `(MIT OR Apache-2.0)` passes when any alternative passes, and an
 * alternative `MIT AND ISC` passes when every licence it combines is allowed (e.g. victory-vendor, M7 charts).
 */
function allowed(name, license) {
  const alternatives = license.replace(/[()]/g, '').split(/\s+OR\s+/);
  return alternatives.some((alt) =>
    alt.split(/\s+AND\s+/).every((id) => ALLOWED.has(id) || (FONT_ONLY[id] ?? []).includes(name)),
  );
}

export function check(packages) {
  const failures = [];
  for (const pkg of packages) {
    if (pkg.private) continue;
    const license = spdx(pkg.license);
    if (allowed(pkg.name, license) || EXCEPTIONS.has(pkg.name)) continue;
    failures.push(`${pkg.name}@${pkg.version}: ${license}`);
  }
  return failures;
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const cwd = process.argv[2] ? resolve(process.argv[2]) : ROOT;
  const out = execFileSync('npm', ['query', '.prod'], { cwd, encoding: 'utf8' });
  const packages = JSON.parse(out);
  const failures = check(packages);
  if (failures.length > 0) {
    console.error('licenses: not allowed:');
    for (const f of failures) console.error(`  ${f}`);
    process.exit(1);
  }
  console.log(`licenses: ok (${packages.filter((p) => !p.private).length} production packages)`);
}
