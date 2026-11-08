import { useT } from '@porto/shared/i18n';
import { formatCents } from '@porto/shared/lib/format';

/** An amount in integer cents, formatted at the edge in the page language. */
export function Money({ cents, currency = 'EUR' }: { cents: number; currency?: string }) {
  const { i18n } = useT();
  return <span className="tabular-nums">{formatCents(cents, currency, i18n.language)}</span>;
}
