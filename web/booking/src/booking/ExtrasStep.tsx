import { LockIcon, TagIcon } from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';

import {
  isProblem,
  useBookingQuote,
  type BookingConfig,
  type BookingQuoteInput,
  type BookingServiceOption,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Alert, AlertDescription } from '@porto/shared/ui/alert';
import { Button } from '@porto/shared/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@porto/shared/ui/card';
import { Checkbox } from '@porto/shared/ui/checkbox';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { Label } from '@porto/shared/ui/label';
import { ProblemAlert } from '@porto/shared/ui/patterns/ProblemAlert';
import { Skeleton } from '@porto/shared/ui/skeleton';

import { localized } from '../common/i18nText';
import { Money } from '../common/Money';
import { CancellationTiers, PriceBreakdown } from '../common/PriceBreakdown';
import { effectiveServices, quoteInput } from './quote';
import type { SearchInput } from './state';

function useDebounced<T>(value: T, ms: number): T {
  const [v, setV] = useState(value);
  useEffect(() => {
    const id = window.setTimeout(() => setV(value), ms);
    return () => window.clearTimeout(id);
  }, [value, ms]);
  return v;
}

/** Step 3 "Extra e riepilogo": online services, promo code, the price (debounced quote) and "Blocca il posto". */
export function ExtrasStep({
  slug,
  config,
  search,
  category,
  services,
  servicesTouched,
  promo,
  holding,
  holdError,
  onServices,
  onPromo,
  onHold,
}: {
  slug: string;
  config: BookingConfig;
  search: SearchInput;
  category: string;
  services: Record<string, number>;
  servicesTouched: boolean;
  promo: string;
  holding: boolean;
  holdError: unknown;
  onServices: (s: Record<string, number>) => void;
  onPromo: (code: string) => void;
  onHold: () => void;
}) {
  const { t, i18n } = useT();
  const selected = effectiveServices(config.services, services, servicesTouched);
  const inputKey = JSON.stringify(quoteInput(search, category, selected, promo));
  const debouncedKey = useDebounced(inputKey, 300);
  const debounced = useMemo(() => JSON.parse(debouncedKey) as BookingQuoteInput, [debouncedKey]);
  const quote = useBookingQuote(slug, debounced);
  const [promoText, setPromoText] = useState(promo);
  const categoryName = config.categories.find((c) => c.code === category)?.name;

  const toggle = (o: BookingServiceOption, on: boolean) => {
    const next = { ...selected };
    if (on) next[o.item_id] = next[o.item_id] ?? 1000;
    else delete next[o.item_id];
    onServices(next);
  };
  const setQty = (o: BookingServiceOption, units: number) => {
    const max = o.max_qty_milli ?? 1000;
    const milli = Math.min(Math.max(1, Math.trunc(units)) * 1000, max);
    onServices({ ...selected, [o.item_id]: milli });
  };

  const promoRejected =
    (promo !== '' && quote.data && !quote.isPlaceholderData && !quote.data.promo_applied) ||
    isProblem(quote.error, 'promo_not_applicable');

  return (
    <div className="flex flex-col gap-4">
      {config.services.length > 0 ? (
        <Card>
          <CardHeader>
            <CardTitle>{t('checkout:extras.services')}</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-3">
            {config.services.map((o) => {
              const id = `service-${o.item_id}`;
              const qty = selected[o.item_id];
              const multi = (o.max_qty_milli ?? 1000) > 1000;
              return (
                <div
                  key={o.item_id}
                  className="flex flex-wrap items-start gap-3"
                  data-testid="booking-service"
                >
                  <Checkbox
                    id={id}
                    checked={qty !== undefined}
                    onCheckedChange={(v) => toggle(o, v === true)}
                  />
                  <div className="flex min-w-0 flex-1 flex-col gap-0.5">
                    <Label htmlFor={id}>{localized(o.name, i18n.language)}</Label>
                    {o.description ? (
                      <span className="text-muted-foreground text-xs">
                        {localized(o.description, i18n.language)}
                      </span>
                    ) : null}
                    {o.unit_price_cents !== undefined ? (
                      <span className="text-muted-foreground text-xs">
                        <Money cents={o.unit_price_cents} currency={config.currency} />{' '}
                        {t(`checkout:extras.unit.${o.unit}`, { defaultValue: '' })}
                      </span>
                    ) : null}
                  </div>
                  {multi && qty !== undefined ? (
                    <Input
                      type="number"
                      inputMode="numeric"
                      min={1}
                      max={Math.trunc((o.max_qty_milli ?? 1000) / 1000)}
                      value={Math.trunc(qty / 1000)}
                      onChange={(e) => setQty(o, Number(e.target.value) || 1)}
                      aria-label={t('checkout:extras.quantity', {
                        name: localized(o.name, i18n.language),
                      })}
                      className="w-20"
                    />
                  ) : null}
                </div>
              );
            })}
          </CardContent>
        </Card>
      ) : null}

      <form
        className="flex items-end gap-2"
        onSubmit={(e) => {
          e.preventDefault();
          onPromo(promoText.trim());
        }}
      >
        <Field className="flex-1">
          <FieldLabel htmlFor="promo-code">{t('checkout:extras.promo')}</FieldLabel>
          <Input
            id="promo-code"
            value={promoText}
            maxLength={64}
            autoComplete="off"
            onChange={(e) => setPromoText(e.target.value)}
          />
        </Field>
        <Button type="submit" variant="outline">
          <TagIcon />
          {t('checkout:extras.applyPromo')}
        </Button>
      </form>
      {promoRejected ? (
        <p className="text-destructive text-sm" role="status">
          {t('checkout:extras.promoRejected')}
        </p>
      ) : null}

      <Card>
        <CardHeader>
          <CardTitle>
            {t('checkout:extras.summary', {
              category: localized(categoryName, i18n.language) || category,
            })}
          </CardTitle>
        </CardHeader>
        <CardContent className="flex flex-col gap-4" aria-busy={quote.isFetching}>
          {quote.data ? (
            <>
              <PriceBreakdown quote={quote.data} />
              <CancellationTiers tiers={quote.data.cancellation_tiers} />
              {quote.data.approval_required ? (
                <Alert>
                  <AlertDescription>{t('checkout:extras.approvalNotice')}</AlertDescription>
                </Alert>
              ) : null}
            </>
          ) : quote.error && !isProblem(quote.error, 'promo_not_applicable') ? (
            <ProblemAlert error={quote.error} />
          ) : (
            <Skeleton className="h-32 w-full" aria-label={t('checkout:extras.pricing')} />
          )}
        </CardContent>
      </Card>

      {holdError ? <ProblemAlert error={holdError} /> : null}
      {!config.payable ? (
        <Alert>
          <AlertDescription>{t('checkout:extras.notPayable')}</AlertDescription>
        </Alert>
      ) : null}
      <Button
        size="lg"
        onClick={onHold}
        disabled={holding || !quote.data || !config.payable}
        data-testid="booking-hold"
      >
        <LockIcon />
        {holding ? t('checkout:extras.holding') : t('checkout:extras.hold')}
      </Button>
    </div>
  );
}
