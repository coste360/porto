import { cn } from '@porto/shared/lib/utils';

import coste360Primary from '../../brand/logo-primary.svg';
import coste360White from '../../brand/logo-white.svg';
import { useT } from '../../i18n/useT';

export const COSTE360_URL = 'https://coste360.it';

/** Official Coste360 logo files (assets/brand/coste360/), never redrawn or recoloured. */
export const COSTE360_FILES = { light: coste360Primary, dark: coste360White } as const;

export type MadeByCoste360Props = { className?: string };

/**
 * Credits the maker: "un prodotto di" + the Coste360 logo, linking to coste360.it in a new tab
 * (ADR 0007, D1). The link carries the accessible name, so both images are decorative.
 */
export function MadeByCoste360({ className }: MadeByCoste360Props) {
  const { t } = useT();
  const imgClass = 'block h-4 w-auto';
  return (
    <a
      href={COSTE360_URL}
      target="_blank"
      rel="noopener noreferrer"
      aria-label={t('common:product.madeByLink')}
      title={t('common:product.madeByLink')}
      data-slot="made-by-coste360"
      className={cn(
        'text-muted-foreground hover:text-foreground focus-visible:ring-ring inline-flex w-fit items-center gap-2 rounded-md px-2 py-1 text-xs outline-none focus-visible:ring-2',
        className,
      )}
    >
      <span>{t('common:product.madeBy')}</span>
      <img src={COSTE360_FILES.light} alt="" className={cn(imgClass, 'dark:hidden')} />
      <img src={COSTE360_FILES.dark} alt="" className={cn(imgClass, 'hidden dark:block')} />
    </a>
  );
}
