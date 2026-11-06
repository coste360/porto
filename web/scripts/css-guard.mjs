#!/usr/bin/env node
// CSS guard, run by `npm run lint`. Porto styling is shadcn/ui components plus Tailwind
// utilities on the theme tokens of shared/src/styles/globals.css (AGENTS.md, ADR 0007).
// It fails on:
//   - any stylesheet other than the theme file, and CSS imports other than the theme;
//   - inline `style={...}`, `<style>`, `dangerouslySetInnerHTML`, `.style.` / `.style[`
//     assignments, `cssText`, `setProperty`, `insertRule`;
//   - hex / rgb / hsl / oklch colour literals in .ts/.tsx (tests excluded: they hold
//     reference vectors such as the WCAG black/white pair, never styling);
//   - Tailwind arbitrary values (`w-[13px]`, `bg-[#fff]`, `[--x:1]`) in source files.
//     Variants such as `data-[state=open]:` and selectors such as `[&>svg]:` stay allowed;
//   - `<style>` and `style=""` in .html files.
import { readFileSync, readdirSync, statSync } from 'node:fs';
import { dirname, join, relative, sep } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

export const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..');
const SCAN = ['admin', 'booking', 'shared'];
const SKIP_DIRS = new Set(['node_modules', 'dist', 'coverage', 'generated']);

export const THEME_FILE = 'shared/src/styles/globals.css';
const THEME_IMPORT = '@porto/shared/styles/globals.css';

/**
 * Files produced by the shadcn CLI (`npx shadcn@latest add`), kept as generated: their
 * internals use inline CSS variables and arbitrary values by design. Page code may not.
 */
export const VENDORED = [/^shared\/src\/ui\/[^/]+\.tsx$/, /^shared\/src\/hooks\/[^/]+\.ts$/];

/**
 * Explicit exemptions, `path: reason`. Empty in M0: adding one needs a reason and review.
 */
export const EXEMPT = new Map([]);

const CODE_EXT = /\.(ts|tsx|js|jsx|mjs)$/;
const COLOUR_EXT = /(?<!\.test)\.(ts|tsx)$/;
const STYLESHEET_EXT = /\.(css|scss|sass|less|styl)$/;

const RULES = [
  { id: 'inline-style', re: /\bstyle=\{/, ext: /\.(tsx|jsx)$/ },
  { id: 'style-tag', re: /<style[\s>]/, ext: CODE_EXT },
  { id: 'inner-html', re: /dangerouslySetInnerHTML/, ext: CODE_EXT },
  { id: 'style-property', re: /\.style\s*(\.|\[)/, ext: CODE_EXT },
  { id: 'css-text', re: /\b(cssText|insertRule)\b|\.setProperty\(/, ext: CODE_EXT },
  {
    id: 'hex-colour',
    re: /(?<![\w&/])#(?:[0-9a-fA-F]{8}|[0-9a-fA-F]{6}|[0-9a-fA-F]{3,4})\b/,
    ext: COLOUR_EXT,
  },
  { id: 'colour-function', re: /\b(?:rgba?|hsla?|oklch|oklab|lab|lch)\(/, ext: COLOUR_EXT },
  {
    id: 'css-import',
    re: /import\s+(?:[^'"]*from\s+)?['"]([^'"]+\.(?:css|scss|sass|less))['"]/,
    ext: CODE_EXT,
    allow: (m) => m[1] === THEME_IMPORT,
  },
  {
    // A class token ending in an arbitrary value: `w-[3px]`, `bg-[#fff]`, `[--x:1]`.
    id: 'arbitrary-value',
    re: /(?:^|[\s"'`{])(!?(?:[\w-]+:)*(?:[\w-]+-\[[^\]\s]+\]|\[[a-z-]+:[^\]\s]+\]))(?=[\s"'`}]|$)/m,
    ext: /\.(tsx|jsx|ts)$/,
  },
];

const HTML_RULES = [
  { id: 'html-style-tag', re: /<style[\s>]/i },
  { id: 'html-style-attr', re: /\sstyle\s*=/i },
];

function toPosix(path) {
  return path.split(sep).join('/');
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
    else out.push(full);
  }
  return out;
}

/** Checks one file's text; returns a list of `{ rule, line }` violations. */
export function checkSource(path, text) {
  const violations = [];
  if (EXEMPT.has(path)) return violations;
  if (STYLESHEET_EXT.test(path)) {
    if (path !== THEME_FILE) violations.push({ rule: 'stylesheet', line: 1 });
    return violations;
  }
  if (path.endsWith('.html')) {
    text.split('\n').forEach((line, i) => {
      for (const rule of HTML_RULES) {
        if (rule.re.test(line)) violations.push({ rule: rule.id, line: i + 1 });
      }
    });
    return violations;
  }
  if (!CODE_EXT.test(path)) return violations;
  const vendored = VENDORED.some((re) => re.test(path));
  text.split('\n').forEach((line, i) => {
    for (const rule of RULES) {
      if (!rule.ext.test(path)) continue;
      if (vendored && rule.id !== 'css-import') continue;
      const m = rule.re.exec(line);
      if (m && !(rule.allow && rule.allow(m))) violations.push({ rule: rule.id, line: i + 1 });
    }
  });
  return violations;
}

export function run(root = ROOT) {
  const failures = [];
  for (const top of SCAN) {
    for (const file of walk(join(root, top), [])) {
      const path = toPosix(relative(root, file));
      if (!CODE_EXT.test(path) && !STYLESHEET_EXT.test(path) && !path.endsWith('.html')) continue;
      for (const v of checkSource(path, readFileSync(file, 'utf8'))) {
        failures.push(`${path}:${v.line}: ${v.rule}`);
      }
    }
  }
  return failures;
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const failures = run();
  if (failures.length > 0) {
    console.error('css-guard: custom styling is not allowed (use shadcn + theme tokens):');
    for (const f of failures) console.error(`  ${f}`);
    process.exit(1);
  }
  console.log('css-guard: ok');
}
