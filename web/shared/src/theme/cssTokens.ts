/** Parses `--name: value;` declarations of a CSS block (`:root` or `.dark`) in globals.css. */
export function parseBlock(css: string, selector: ':root' | '.dark'): Record<string, string> {
  const escaped = selector.replace('.', '\\.');
  const match = new RegExp(`(?:^|\\n)${escaped}\\s*\\{([^}]*)\\}`).exec(css);
  if (!match?.[1]) throw new Error(`block ${selector} not found`);
  const vars: Record<string, string> = {};
  for (const decl of match[1].matchAll(/--([\w-]+)\s*:\s*([^;]+);/g)) {
    if (decl[1] && decl[2]) vars[decl[1]] = decl[2].trim();
  }
  return vars;
}
