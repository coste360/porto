import type { BookingQuote, CancellationTier } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatBp, formatQtyMilli } from '@porto/shared/lib/format';
import { Separator } from '@porto/shared/ui/separator';

import { localized } from './i18nText';
import { Money } from './Money';

/** Price lines, VAT, total and the amount due now of a quote (binding only once the booking is created). */
export function PriceBreakdown({
  quote,
  showDueNow = true,
}: {
  quote: BookingQuote;
  showDueNow?: boolean;
}) {
  const { t, i18n } = useT();
  const c = quote.currency;
  return (
    <div className="flex flex-col gap-2 text-sm" data-testid="price-breakdown">
      <ul className="flex flex-col gap-1.5">
        {quote.lines.map((l, i) => (
          <li
            key={`${l.kind}-${l.item_id ?? ''}-${i}`}
            className="flex items-start justify-between gap-3"
          >
            <span className="min-w-0">
              {localized(l.label, i18n.language)}
              {l.kind === 'service' && l.qty_milli !== 1000 ? (
                <span className="text-muted-foreground">
                  {' '}
                  {t('checkout:price.qty', { qty: formatQtyMilli(l.qty_milli, i18n.language) })}
                </span>
              ) : null}
            </span>
            <Money cents={l.total_cents} currency={c} />
          </li>
        ))}
      </ul>
      <Separator />
      <div className="text-muted-foreground flex justify-between gap-3">
        <span>{t('checkout:price.net')}</span>
        <Money cents={quote.net_cents} currency={c} />
      </div>
      <div className="text-muted-foreground flex justify-between gap-3">
        <span>{t('checkout:price.vat')}</span>
        <Money cents={quote.vat_cents} currency={c} />
      </div>
      <div className="flex justify-between gap-3 text-base font-semibold">
        <span>{t('checkout:price.total')}</span>
        <span data-testid="price-total">
          <Money cents={quote.total_cents} currency={c} />
        </span>
      </div>
      {showDueNow && quote.due_now_cents !== quote.total_cents ? (
        <div className="flex justify-between gap-3 font-medium">
          <span>{t(`checkout:price.dueNow.${quote.payment_mode}`)}</span>
          <span data-testid="price-due-now">
            <Money cents={quote.due_now_cents} currency={c} />
          </span>
        </div>
      ) : null}
    </div>
  );
}

/** The cancellation tiers of the policy, most generous first. */
export function CancellationTiers({ tiers }: { tiers: CancellationTier[] }) {
  const { t, i18n } = useT();
  if (tiers.length === 0) return null;
  const sorted = [...tiers].sort((a, b) => b.min_days_before - a.min_days_before);
  return (
    <div className="flex flex-col gap-1 text-sm" data-testid="cancellation-tiers">
      <p className="font-medium">{t('checkout:cancellation.title')}</p>
      <ul className="text-muted-foreground flex flex-col gap-0.5">
        {sorted.map((tier) => (
          <li key={tier.min_days_before}>
            {t(
              tier.refund_bp === 0
                ? 'checkout:cancellation.noRefund'
                : 'checkout:cancellation.tier',
              {
                count: tier.min_days_before,
                percent: formatBp(tier.refund_bp, i18n.language),
              },
            )}
          </li>
        ))}
      </ul>
    </div>
  );
}
