import { Link, getRouteApi } from '@tanstack/react-router';
import { CircleCheckIcon } from 'lucide-react';
import { useState } from 'react';

import {
  usePriceCoverage,
  usePricingToolItems,
  usePricingToolPriceList,
  type PriceCoverage,
  type PriceListDocument,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatIsoDate } from '@porto/shared/lib/format';
import { Button } from '@porto/shared/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@porto/shared/ui/card';
import { Label } from '@porto/shared/ui/label';
import { DatePicker, EmptyState, PageHeader } from '@porto/shared/ui/patterns';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@porto/shared/ui/select';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@porto/shared/ui/table';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';
import { localName } from '../../maps/lib';
import { isNoPriceReason } from '../lib/enums';
import { NO_FILTERS, filterGaps, filterOptions, gapRow, type CoverageFilters } from './coverageFilter';

const route = getRouteApi('/auth/m/$marinaId/price-lists/$priceListId/coverage');
const ALL = '__all__';

export function CoveragePage() {
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
        <CoverageView doc={list.data} />
      )}
    </RequirePermission>
  );
}

function CoverageView({ doc }: { doc: PriceListDocument }) {
  const { t, i18n } = useT();
  const { marinaId } = useCurrentMarina();
  const [quotedOn, setQuotedOn] = useState<string | null>(null);
  const coverage = usePriceCoverage(marinaId, doc.id, quotedOn ?? undefined);
  return (
    <div className="flex flex-col gap-4">
      <PageHeader
        title={t('pricingTools:coverage.heading', { code: doc.code })}
        description={`${localName(doc.name, i18n.language)} · ${formatIsoDate(doc.valid_from)} – ${formatIsoDate(doc.valid_to)}`}
        actions={
          <Button variant="outline" asChild>
            <Link
              to="/m/$marinaId/price-lists/$priceListId/matrix"
              params={{ marinaId, priceListId: doc.id }}
            >
              {t('pricingTools:matrix.open')}
            </Link>
          </Button>
        }
      />
      <div className="flex w-48 flex-col gap-1">
        <Label htmlFor="coverage-quoted-on">{t('pricingTools:simulator.quotedOn')}</Label>
        <DatePicker
          id="coverage-quoted-on"
          value={quotedOn}
          onChange={setQuotedOn}
          placeholder={t('pricingTools:simulator.today')}
        />
      </div>
      {coverage.isPending ? (
        <LoadingState rows={6} />
      ) : coverage.isError ? (
        <QueryErrorState error={coverage.error} onRetry={() => void coverage.refetch()} />
      ) : (
        <CoverageReport doc={doc} coverage={coverage.data} />
      )}
    </div>
  );
}

/** Summary cards, filterable gaps table and the elements without a category (exported for the tests). */
export function CoverageReport({
  doc,
  coverage,
}: {
  doc: PriceListDocument;
  coverage: PriceCoverage;
}) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const items = usePricingToolItems(marinaId);
  const [filters, setFilters] = useState<CoverageFilters>(NO_FILTERS);
  const options = filterOptions(coverage.gaps);
  const gaps = filterGaps(coverage.gaps, filters);
  const without = coverage.elements_without_category;
  const withoutTotal = without.berths + without.dynamic_areas + without.land_resources;
  const itemCode = (id: string) => items.data?.items.find((i) => i.id === id)?.code ?? id;

  return (
    <div className="flex flex-col gap-4">
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <SummaryCard
          testId="coverage-checked"
          title={t('pricingTools:coverage.checked')}
          value={coverage.checked}
        />
        <SummaryCard
          testId="coverage-gaps"
          title={t('pricingTools:coverage.gaps')}
          value={coverage.gaps.length}
        />
        <SummaryCard
          testId="coverage-without-category"
          title={t('pricingTools:coverage.withoutCategory')}
          value={withoutTotal}
          description={t('pricingTools:coverage.withoutCategoryDetail', {
            berths: without.berths,
            areas: without.dynamic_areas,
            land: without.land_resources,
          })}
        />
        <SummaryCard
          testId="coverage-unknown"
          title={t('pricingTools:coverage.unknownCategories')}
          value={coverage.unknown_categories.length}
          description={coverage.unknown_categories.map((c) => `${c.code} (${c.count})`).join(', ')}
        />
      </div>

      {withoutTotal > 0 || coverage.unknown_categories.length > 0 ? (
        <div className="flex flex-wrap gap-2">
          {withoutTotal > 0 ? (
            <Button variant="outline" asChild>
              <Link to="/m/$marinaId/maps" params={{ marinaId }}>
                {t('pricingTools:coverage.openMaps')}
              </Link>
            </Button>
          ) : null}
          {coverage.unknown_categories.length > 0 ? (
            <Button variant="outline" asChild>
              <Link to="/m/$marinaId/pricing/classifications" params={{ marinaId }}>
                {t('pricingTools:coverage.openClassifications')}
              </Link>
            </Button>
          ) : null}
        </div>
      ) : null}

      {coverage.gaps.length === 0 ? (
        <EmptyState
          icon={<CircleCheckIcon />}
          title={t('pricingTools:coverage.noGaps')}
          description={t('pricingTools:coverage.noGapsHint')}
        />
      ) : (
        <Card>
          <CardHeader>
            <CardTitle>{t('pricingTools:coverage.gapsTitle')}</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-4">
            <div
              className="flex flex-wrap items-end gap-4"
              role="group"
              aria-label={t('pricingTools:coverage.filters')}
            >
              <FilterSelect
                id="coverage-contract-type"
                label={t('pricingTools:coverage.contractType')}
                value={filters.contractType}
                options={options.contractTypes.map((c) => ({
                  value: c,
                  label: t(`pricingEnums:contractTypes.${c}`, { defaultValue: c }),
                }))}
                onChange={(v) => setFilters((f) => ({ ...f, contractType: v }))}
              />
              <FilterSelect
                id="coverage-period"
                label={t('pricingTools:coverage.period')}
                value={filters.period}
                options={options.periods.map((p) => ({ value: p, label: p }))}
                onChange={(v) => setFilters((f) => ({ ...f, period: v }))}
              />
              <FilterSelect
                id="coverage-category"
                label={t('pricingTools:coverage.category')}
                value={filters.category}
                options={options.categories.map((c) => ({ value: c, label: c }))}
                onChange={(v) => setFilters((f) => ({ ...f, category: v }))}
              />
            </div>
            <div>
              <Table data-testid="coverage-table">
                <TableHeader>
                  <TableRow>
                    <TableHead>{t('pricingTools:coverage.contractType')}</TableHead>
                    <TableHead>{t('pricingTools:coverage.category')}</TableHead>
                    <TableHead>{t('pricingTools:coverage.period')}</TableHead>
                    <TableHead>{t('pricingTools:coverage.item')}</TableHead>
                    <TableHead>{t('pricingTools:coverage.reason')}</TableHead>
                    <TableHead>
                      <span className="sr-only">{t('pricingTools:coverage.actions')}</span>
                    </TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {gaps.map((g, i) => (
                    <TableRow key={i} data-testid="coverage-gap">
                      <TableCell>{t(`pricingEnums:contractTypes.${g.contract_type}`)}</TableCell>
                      <TableCell>{gapRow(g)}</TableCell>
                      <TableCell>{g.period ?? ''}</TableCell>
                      <TableCell>{itemCode(g.item_id)}</TableCell>
                      <TableCell>
                        {isNoPriceReason(g.reason)
                          ? t(`pricingEnums:noPriceReasons.${g.reason}`)
                          : g.reason}
                      </TableCell>
                      <TableCell>
                        <Link
                          to="/m/$marinaId/price-lists/$priceListId"
                          params={{ marinaId, priceListId: doc.id }}
                          hash="rules"
                          className="underline underline-offset-4"
                        >
                          {t('pricingTools:coverage.openRules')}
                        </Link>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
            {gaps.length === 0 ? (
              <p className="text-sm text-muted-foreground">{t('pricingTools:coverage.noMatch')}</p>
            ) : null}
          </CardContent>
        </Card>
      )}
    </div>
  );
}

function SummaryCard({
  title,
  value,
  description,
  testId,
}: {
  title: string;
  value: number;
  description?: string;
  testId: string;
}) {
  return (
    <Card>
      <CardHeader>
        <CardDescription>{title}</CardDescription>
        <CardTitle className="text-2xl tabular-nums" data-testid={testId}>
          {value}
        </CardTitle>
        {description ? <CardDescription>{description}</CardDescription> : null}
      </CardHeader>
    </Card>
  );
}

function FilterSelect({
  id,
  label,
  value,
  options,
  onChange,
}: {
  id: string;
  label: string;
  value: string;
  options: { value: string; label: string }[];
  onChange: (value: string) => void;
}) {
  const { t } = useT();
  return (
    <div className="flex w-48 flex-col gap-1">
      <Label htmlFor={id}>{label}</Label>
      <Select value={value || ALL} onValueChange={(v) => onChange(v === ALL ? '' : v)}>
        <SelectTrigger id={id} className="w-full">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value={ALL}>{t('pricingTools:coverage.all')}</SelectItem>
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
