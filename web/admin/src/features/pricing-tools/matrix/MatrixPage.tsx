import { Link, getRouteApi, useNavigate } from '@tanstack/react-router';
import { CalculatorIcon, ListChecksIcon, PrinterIcon } from 'lucide-react';

import {
  usePriceMatrix,
  usePricingToolItems,
  usePricingToolPriceList,
  type PriceListDocument,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatIsoDate } from '@porto/shared/lib/format';
import { Button } from '@porto/shared/ui/button';
import { Card, CardContent } from '@porto/shared/ui/card';
import { Label } from '@porto/shared/ui/label';
import { DatePicker, PageHeader } from '@porto/shared/ui/patterns';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@porto/shared/ui/select';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@porto/shared/ui/tabs';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';
import { localName } from '../../maps/lib';
import { PriceMatrixTable } from '../components/PriceMatrixTable';
import { CHANNELS, CUSTOMER_CATEGORIES } from '../lib/enums';
import { encodeProbe } from '../probe';
import { defaultRows, type MatrixSearch } from './matrixSearch';

const route = getRouteApi('/auth/m/$marinaId/price-lists/$priceListId/matrix');
const ANY = '__any__';

export function MatrixPage() {
  const { can, marinaId } = useCurrentMarina();
  const { priceListId } = route.useParams();
  const list = usePricingToolPriceList(marinaId, priceListId);
  return (
    <RequirePermission allowed={can('pricing.read')}>
      {list.isPending ? (
        <LoadingState rows={6} />
      ) : list.isError ? (
        <QueryErrorState error={list.error} onRetry={() => void list.refetch()} />
      ) : (
        <MatrixView doc={list.data} />
      )}
    </RequirePermission>
  );
}

function MatrixView({ doc }: { doc: PriceListDocument }) {
  const { t, i18n } = useT();
  const { marinaId } = useCurrentMarina();
  const search = route.useSearch();
  const navigate = useNavigate({ from: '/m/$marinaId/price-lists/$priceListId/matrix' });
  const items = usePricingToolItems(marinaId);
  const contractType = search.contract_type ?? doc.contract_types[0];
  const rows = search.rows ?? defaultRows(doc);
  const params: MatrixSearch = {
    ...search,
    rows,
    ...(contractType ? { contract_type: contractType } : {}),
  };
  const matrix = usePriceMatrix(marinaId, doc.id, params, !!contractType);
  const setSearch = (patch: Partial<MatrixSearch>) =>
    void navigate({ search: (prev) => ({ ...prev, ...patch }), replace: true });
  const berthFees = (items.data?.items ?? []).filter((i) => i.item_type === 'berth_fee');

  return (
    <div className="flex flex-col gap-4">
      <PageHeader
        title={t('pricingTools:matrix.title', { code: doc.code })}
        description={`${localName(doc.name, i18n.language)} · ${formatIsoDate(doc.valid_from)} – ${formatIsoDate(doc.valid_to)}`}
        actions={
          <div className="flex flex-wrap gap-2">
            <Button variant="outline" asChild>
              <Link
                to="/m/$marinaId/price-lists/$priceListId/matrix/print"
                params={{ marinaId, priceListId: doc.id }}
                search={params}
              >
                <PrinterIcon />
                {t('pricingTools:matrix.print')}
              </Link>
            </Button>
            <Button variant="outline" asChild>
              <Link
                to="/m/$marinaId/price-lists/$priceListId/coverage"
                params={{ marinaId, priceListId: doc.id }}
              >
                <ListChecksIcon />
                {t('pricingTools:coverage.title')}
              </Link>
            </Button>
            <Button variant="outline" asChild>
              <Link to="/m/$marinaId/pricing/simulator" params={{ marinaId }}>
                <CalculatorIcon />
                {t('pricingTools:simulator.title')}
              </Link>
            </Button>
          </div>
        }
      />
      <div
        className="flex flex-wrap items-end gap-4"
        role="group"
        aria-label={t('pricingTools:matrix.options')}
      >
        <ParamSelect
          id="matrix-item"
          label={t('pricingTools:matrix.item')}
          value={search.item_id ?? ''}
          anyLabel={t('pricingTools:simulator.primaryItem')}
          options={berthFees.map((i) => ({
            value: i.id,
            label: `${i.code} · ${localName(i.name, i18n.language)}`,
          }))}
          onChange={(v) => setSearch({ item_id: v || undefined })}
        />
        <ParamSelect
          id="matrix-rows"
          label={t('pricingTools:matrix.rows')}
          value={rows}
          options={[
            { value: 'bands', label: t('pricingTools:matrix.rowsBands') },
            { value: 'categories', label: t('pricingTools:matrix.rowsCategories') },
          ]}
          onChange={(v) => setSearch({ rows: v as MatrixSearch['rows'] })}
        />
        <ParamSelect
          id="matrix-channel"
          label={t('pricingTools:simulator.channel')}
          value={search.channel ?? ''}
          anyLabel={t('pricingEnums:channels.desk')}
          options={CHANNELS.filter((c) => c !== 'desk').map((c) => ({
            value: c,
            label: t(`pricingEnums:channels.${c}`),
          }))}
          onChange={(v) => setSearch({ channel: (v || undefined) as MatrixSearch['channel'] })}
        />
        <ParamSelect
          id="matrix-customer-category"
          label={t('pricingTools:simulator.customerCategory')}
          value={search.customer_category ?? ''}
          anyLabel={t('pricingTools:simulator.noCustomerCategory')}
          options={CUSTOMER_CATEGORIES.map((c) => ({
            value: c,
            label: t(`pricingEnums:customerCategories.${c}`),
          }))}
          onChange={(v) =>
            setSearch({
              customer_category: (v || undefined) as MatrixSearch['customer_category'],
            })
          }
        />
        <div className="flex w-48 flex-col gap-1">
          <Label htmlFor="matrix-quoted-on">{t('pricingTools:simulator.quotedOn')}</Label>
          <DatePicker
            id="matrix-quoted-on"
            value={search.quoted_on ?? null}
            onChange={(d) => setSearch({ quoted_on: d ?? undefined })}
            placeholder={t('pricingTools:simulator.today')}
          />
        </div>
      </div>
      {contractType ? (
        <Tabs
          value={contractType}
          onValueChange={(v) => setSearch({ contract_type: v as MatrixSearch['contract_type'] })}
        >
          <TabsList>
            {doc.contract_types.map((c) => (
              <TabsTrigger key={c} value={c}>
                {t(`pricingEnums:contractTypes.${c}`)}
              </TabsTrigger>
            ))}
          </TabsList>
          {doc.contract_types.map((c) => (
            <TabsContent key={c} value={c}>
              {c === contractType ? (
                <Card>
                  <CardContent>
                    {!contractType ? (
                      <p className="text-sm text-muted-foreground">
                        {t('pricingTools:matrix.noContractTypes')}
                      </p>
                    ) : matrix.isPending ? (
                      <LoadingState rows={4} />
                    ) : matrix.isError ? (
                      <QueryErrorState error={matrix.error} onRetry={() => void matrix.refetch()} />
                    ) : (
                      <>
                        <p className="mb-2 text-sm text-muted-foreground">
                          {t('pricingTools:matrix.hint', {
                            date: formatIsoDate(matrix.data.quoted_on),
                          })}
                        </p>
                        <PriceMatrixTable
                          mode="values"
                          matrix={matrix.data}
                          onCellClick={(probe) =>
                            void navigate({
                              to: '/m/$marinaId/pricing/simulator',
                              params: { marinaId },
                              search: { probe: encodeProbe(probe) },
                            })
                          }
                        />
                      </>
                    )}
                  </CardContent>
                </Card>
              ) : null}
            </TabsContent>
          ))}
        </Tabs>
      ) : (
        <p className="text-sm text-muted-foreground">{t('pricingTools:matrix.noContractTypes')}</p>
      )}
    </div>
  );
}

function ParamSelect({
  id,
  label,
  value,
  options,
  anyLabel,
  onChange,
}: {
  id: string;
  label: string;
  value: string;
  options: { value: string; label: string }[];
  anyLabel?: string;
  onChange: (value: string) => void;
}) {
  return (
    <div className="flex w-56 flex-col gap-1">
      <Label htmlFor={id}>{label}</Label>
      <Select value={value || ANY} onValueChange={(v) => onChange(v === ANY ? '' : v)}>
        <SelectTrigger id={id} className="w-full">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {anyLabel ? <SelectItem value={ANY}>{anyLabel}</SelectItem> : null}
          {options.map((o) => (
            <SelectItem key={o.value} value={o.value}>
              {o.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}
