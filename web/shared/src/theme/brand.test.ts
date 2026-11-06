import { readFileSync, readdirSync } from 'node:fs';
import { join } from 'node:path';

import { describe, expect, it } from 'vitest';

const brandDir = join(__dirname, '..', 'brand');
const kit = join(__dirname, '..', '..', '..', '..', 'assets', 'brand', 'porto');
const ORIGIN_DIRS = ['01_Loghi_SVG', '02_Simbolo_SVG', '03_Icone_App_SVG'];

function original(name: string): Buffer {
  for (const dir of ORIGIN_DIRS) {
    try {
      return readFileSync(join(kit, dir, name));
    } catch {
      // try the next folder
    }
  }
  throw new Error(`${name} has no original in assets/brand/porto`);
}

describe('brand files', () => {
  const copies = readdirSync(brandDir).filter((f) => f.endsWith('.svg'));

  it('ships the expected copies', () => {
    expect(copies.sort()).toEqual(
      [
        'porto_app_maskable.svg',
        'porto_logo_horizontal_inverted_2color.svg',
        'porto_logo_horizontal_navy.svg',
        'porto_logo_horizontal_on_navy.svg',
        'porto_logo_horizontal_primary.svg',
        'porto_logo_horizontal_white.svg',
        'porto_symbol_inverted_2color.svg',
        'porto_symbol_navy.svg',
        'porto_symbol_primary.svg',
        'porto_symbol_white.svg',
      ].sort(),
    );
  });

  it.each(readdirSync(brandDir).filter((f) => f.endsWith('.svg')))(
    '%s is byte-identical to the original',
    (name) => {
      expect(readFileSync(join(brandDir, name)).equals(original(name))).toBe(true);
    },
  );
});
