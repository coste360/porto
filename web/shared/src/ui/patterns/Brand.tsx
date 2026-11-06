import { cn } from '@porto/shared/lib/utils';

import horizontalInverted from '../../brand/porto_logo_horizontal_inverted_2color.svg';
import horizontalOnNavy from '../../brand/porto_logo_horizontal_on_navy.svg';
import horizontalPrimary from '../../brand/porto_logo_horizontal_primary.svg';
import symbolInverted from '../../brand/porto_symbol_inverted_2color.svg';
import symbolPrimary from '../../brand/porto_symbol_primary.svg';

export type BrandLogoProps = {
  variant?: 'horizontal' | 'symbol';
  /** `auto` follows the theme; the others pick the file for a known background. */
  tone?: 'auto' | 'onLight' | 'onDark' | 'onNavy';
  size?: 'sm' | 'md' | 'lg';
  className?: string;
};

/** The only way to show the Porto mark (ADR 0007): official files, never redrawn. */
export const BRAND_FILES = {
  horizontal: { light: horizontalPrimary, dark: horizontalInverted, navy: horizontalOnNavy },
  symbol: { light: symbolPrimary, dark: symbolInverted, navy: symbolInverted },
} as const;

// Minimum sizes from the guidelines: horizontal >= 120 px wide, symbol >= 24 px.
const SIZE_CLASSES = {
  horizontal: { sm: 'w-30 min-w-30', md: 'w-40 min-w-30', lg: 'w-56 min-w-30' },
  symbol: { sm: 'size-6 min-w-6', md: 'size-8 min-w-6', lg: 'size-12 min-w-6' },
} as const;

// Clear space: about a quarter of the symbol width around the mark.
const PADDING = { sm: 'p-1', md: 'p-2', lg: 'p-3' } as const;

const BRAND_NAME = 'Porto';

export function BrandLogo({
  variant = 'horizontal',
  tone = 'auto',
  size = 'md',
  className,
}: BrandLogoProps) {
  const files = BRAND_FILES[variant];
  const imgClass = cn('block h-auto', SIZE_CLASSES[variant][size]);
  const wrapper = cn('inline-flex shrink-0', PADDING[size], className);

  if (tone === 'auto') {
    return (
      <span className={wrapper} data-slot="brand-logo">
        <img src={files.light} alt={BRAND_NAME} className={cn(imgClass, 'dark:hidden')} />
        <img src={files.dark} alt={BRAND_NAME} className={cn(imgClass, 'hidden dark:block')} />
      </span>
    );
  }
  const src = tone === 'onLight' ? files.light : tone === 'onDark' ? files.dark : files.navy;
  return (
    <span className={wrapper} data-slot="brand-logo">
      <img src={src} alt={BRAND_NAME} className={imgClass} />
    </span>
  );
}
