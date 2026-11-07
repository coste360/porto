import { getRouteApi } from '@tanstack/react-router';
import { CircleAlertIcon, CopyIcon } from 'lucide-react';
import { useEffect, useRef, useState } from 'react';
import { toast } from 'sonner';

import {
  isProblem,
  pricingProblemDetails,
  usePricingToolItems,
  usePricingToolPriceList,
  useSimulatePrice,
  type PricingSimulateRequest,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatIsoDate } from '@porto/shared/lib/format';
import { Alert, AlertDescription, AlertTitle } from '@porto/shared/ui/alert';
import { Button } from '@porto/shared/ui/button';
import { PageHeader, ProblemAlert } from '@porto/shared/ui/patterns';

import { useCurrentMarina } from '../../../auth/hooks';
import { RequirePermission } from '../../../components/PageStates';
import { useMarinaToday } from '../../registry/lib/today';
import { BreakdownView } from '../components/BreakdownView';
import { isNoPriceReason } from '../lib/enums';
import { decodeProbe } from '../probe';
import { emptySimulatorValues, fromSimulateRequest } from './simulatorValues';
import { SimulatorForm } from './SimulatorForm';

const route = getRouteApi('/auth/m/$marinaId/pricing/simulator');

export function SimulatorPage() {
  const { t } = useT();
  const { can } = useCurrentMarina();
  return (
    <RequirePermission allowed={can('pricing.read')}>
      <PageHeader
        title={t('pricingTools:simulator.title')}
        description={t('pricingTools:simulator.description')}
      />
      <Simulator />
    </RequirePermission>
  );
}

function Simulator() {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const today = useMarinaToday();
  const { probe } = route.useSearch();
  const [initial] = useState(() => {
    const decoded = probe ? decodeProbe(probe) : null;
    return {
      probe: decoded,
      values: decoded ? fromSimulateRequest(decoded) : emptySimulatorValues(),
    };
  });
  const simulate = useSimulatePrice(marinaId);
  const [request, setRequest] = useState<PricingSimulateRequest | null>(null);
  const run = (req: PricingSimulateRequest) => {
    setRequest(req);
    simulate.mutate(req);
  };

  // A probe from the matrix is priced at once: the same request the matrix cell was computed with.
  const ran = useRef(false);
  useEffect(() => {
    if (ran.current || !initial.probe) return;
    ran.current = true;
    run(initial.probe);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const breakdown = simulate.data;
  const items = usePricingToolItems(marinaId);
  const list = usePricingToolPriceList(marinaId, breakdown?.price_list_id ?? null);

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(
        JSON.stringify({ request, breakdown: breakdown ?? null }, null, 2),
      );
      toast.success(t('pricingTools:simulator.copied'));
    } catch {
      toast.error(t('pricingTools:simulator.copyFailed'));
    }
  };

  return (
    <div className="flex flex-col gap-6">
      <div>
        {probe && !initial.probe ? (
          <Alert className="mb-4">
            <CircleAlertIcon />
            <AlertDescription>{t('pricingTools:simulator.badProbe')}</AlertDescription>
          </Alert>
        ) : null}
        <SimulatorForm
          defaultValues={{ ...initial.values, quoted_on: initial.values.quoted_on ?? null }}
          pending={simulate.isPending}
          onSubmit={run}
        />
        <p className="mt-2 text-sm text-muted-foreground">
          {t('pricingTools:simulator.todayHint', { date: formatIsoDate(today) })}
        </p>
      </div>
      <section aria-label={t('pricingTools:simulator.result')} className="flex flex-col gap-4">
        <div className="flex items-center justify-between gap-2">
          <h2 className="text-lg font-semibold">{t('pricingTools:simulator.result')}</h2>
          {request ? (
            <Button variant="outline" size="sm" onClick={() => void copy()}>
              <CopyIcon />
              {t('pricingTools:simulator.copyJson')}
            </Button>
          ) : null}
        </div>
        {simulate.isError ? <SimulateProblem error={simulate.error} /> : null}
        {breakdown && !simulate.isError ? (
          <BreakdownView
            breakdown={breakdown}
            marinaId={marinaId}
            items={items.data?.items ?? []}
            rules={list.data?.rules ?? []}
          />
        ) : null}
        {!breakdown && !simulate.isError && !simulate.isPending ? (
          <p className="text-sm text-muted-foreground">{t('pricingTools:simulator.empty')}</p>
        ) : null}
      </section>
    </div>
  );
}

/** no_price shows its reason and where it happened; promo_not_applicable its own message. */
function SimulateProblem({ error }: { error: unknown }) {
  const { t } = useT();
  if (isProblem(error, 'no_price')) {
    const d = pricingProblemDetails(error) ?? {};
    const facts: [string, string][] = [];
    if (d.contract_type)
      facts.push([
        t('pricingTools:noPrice.contractType'),
        t(`pricingEnums:contractTypes.${String(d.contract_type)}`, {
          defaultValue: String(d.contract_type),
        }),
      ]);
    if (d.night) facts.push([t('pricingTools:noPrice.night'), formatIsoDate(String(d.night))]);
    if (d.unit)
      facts.push([
        t('pricingTools:noPrice.unit'),
        t(`pricingEnums:timeUnits.${String(d.unit)}`, { defaultValue: String(d.unit) }),
      ]);
    if (d.category) facts.push([t('pricingTools:noPrice.category'), String(d.category)]);
    if (d.band) facts.push([t('pricingTools:noPrice.band'), String(d.band)]);
    return (
      <Alert variant="destructive" data-testid="simulator-no-price">
        <CircleAlertIcon />
        <AlertTitle>{t('errors:no_price')}</AlertTitle>
        <AlertDescription>
          {isNoPriceReason(d.reason) ? (
            <p data-testid="no-price-reason">{t(`pricingEnums:noPriceReasons.${d.reason}`)}</p>
          ) : null}
          {facts.length > 0 ? (
            <ul className="list-disc pl-4">
              {facts.map(([k, v]) => (
                <li key={k}>
                  {k}: {v}
                </li>
              ))}
            </ul>
          ) : null}
        </AlertDescription>
      </Alert>
    );
  }
  if (isProblem(error, 'promo_not_applicable')) {
    return (
      <Alert variant="destructive" data-testid="simulator-promo">
        <CircleAlertIcon />
        <AlertTitle>{t('pricingTools:simulator.promoTitle')}</AlertTitle>
        <AlertDescription>{t('errors:promo_not_applicable')}</AlertDescription>
      </Alert>
    );
  }
  return <ProblemAlert error={error} />;
}
