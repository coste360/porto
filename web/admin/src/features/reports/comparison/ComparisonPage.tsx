import { getRouteApi, useNavigate } from '@tanstack/react-router';
import { DownloadIcon } from 'lucide-react';
import { CartesianGrid, Line, LineChart, XAxis, YAxis } from 'recharts';

import {
  useComparison,
  useComparisonFile,
  type ComparisonIndicator,
  type ComparisonResult,
  type ReportAlign,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { saveBlob } from '@porto/shared/lib/download';
import { formatBp, formatIsoDate, formatReportValue } from '@porto/shared/lib/format';
import { Button } from '@porto/shared/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@porto/shared/ui/card';
import {
  ChartContainer,
  ChartLegend,
  ChartLegendContent,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig,
} from '@porto/shared/ui/chart';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { PageHeader, ProblemAlert } from '@porto/shared/ui/patterns';
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
  TableFooter,
  TableHead,
  TableHeader,
  TableRow,
} from '@porto/shared/ui/table';
import { ToggleGroup, ToggleGroupItem } from '@porto/shared/ui/toggle-group';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';
import { PeriodSelector } from '../../dashboard/components/PeriodSelector';
import {
  PERIOD_PRESETS,
  presetPeriod,
  validPeriod,
  type Period,
  type PeriodPreset,
} from '../../dashboard/lib/period';
import { useMarinaToday } from '../../registry/lib/today';
import {
  ALIGNS,
  GRANULARITIES,
  MAX_YEARS_BACK,
  allowedIndicators,
  bucketLabel,
  comparisonFileName,
  comparisonRows,
  parseComparisonSearch,
} from './comparison';

const route = getRouteApi('/auth/m/$marinaId/reports/comparison');
const AUTO = 'auto';

const chartConfig = {
  current: { label: 'current', color: 'var(--chart-1)' },
  previous: { label: 'previous', color: 'var(--chart-2)' },
} satisfies ChartConfig;

/** The preset whose period equals from/to (custom otherwise). */
function presetOf(period: Period, today: string): PeriodPreset {
  return (
    PERIOD_PRESETS.find((p) => {
      if (p === 'custom') return false;
      const q = presetPeriod(p, today);
      return q.from === period.from && q.to === period.to;
    }) ?? 'custom'
  );
}

/**
 * Period comparison (spec 21.3 "Comparazione delle statistiche", m7-plan §5, §10.1): one indicator over the
 * chosen period against 1–5 previous years aligned by date or weekday, as a line chart and a delta table, with
 * CSV and Excel export. All state lives in the URL search.
 */
export function ComparisonPage() {
  const { t } = useT();
  const { can } = useCurrentMarina();
  const indicators = allowedIndicators(can('reports.all'), can('reports.operational'));
  return (
    <RequirePermission allowed={indicators.length > 0}>
      <PageHeader
        title={t('dashboard:comparison.title')}
        description={t('dashboard:comparison.description')}
      />
      <ComparisonView indicators={indicators} />
    </RequirePermission>
  );
}

function ComparisonView({ indicators }: { indicators: ComparisonIndicator[] }) {
  const { t, i18n } = useT();
  const { marinaId } = useCurrentMarina();
  const today = useMarinaToday();
  const raw = route.useSearch();
  const navigate = useNavigate({ from: '/m/$marinaId/reports/comparison' });
  const search = parseComparisonSearch(raw);

  const indicator =
    search.indicator && indicators.includes(search.indicator) ? search.indicator : indicators[0]!;
  const ytd = presetPeriod('ytd', today);
  const period: Period = { from: search.from ?? ytd.from, to: search.to ?? ytd.to };
  const preset = presetOf(period, today);
  const yearsBack = search.years_back ?? 1;
  const align: ReportAlign = search.align ?? 'date';
  const ready = validPeriod(period);

  const setSearch = (patch: Record<string, string | undefined>) =>
    void navigate({
      search: (s: Record<string, string>) => {
        const next: Record<string, string> = { ...s };
        for (const [k, v] of Object.entries(patch)) {
          if (v === undefined || v === '') delete next[k];
          else next[k] = v;
        }
        return next;
      },
      replace: true,
    });

  const query = {
    indicator,
    from: period.from,
    to: period.to,
    years_back: yearsBack === 1 ? undefined : String(yearsBack),
    align: align === 'date' ? undefined : align,
    granularity: search.granularity,
    lang: i18n.language,
  };
  const comparison = useComparison(marinaId, { ...query, lang: undefined }, ready);
  const file = useComparisonFile(marinaId);
  const download = (format: 'csv' | 'xlsx') =>
    file.mutate(
      { query, format },
      { onSuccess: (b) => void saveBlob(b, comparisonFileName(period.from, period.to, format)) },
    );

  return (
    <div className="flex flex-col gap-4" data-testid="comparison-page">
      <div className="flex flex-col gap-3 lg:flex-row lg:flex-wrap lg:items-end">
        <Field className="lg:max-w-72">
          <FieldLabel htmlFor="comparison-indicator">
            {t('dashboard:comparison.indicator')}
          </FieldLabel>
          <Select value={indicator} onValueChange={(v) => setSearch({ indicator: v })}>
            <SelectTrigger id="comparison-indicator" className="min-h-11 w-full sm:min-h-9">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {indicators.map((k) => (
                <SelectItem key={k} value={k}>
                  {t(`dashboard:comparison.indicators.${k}`)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
        <PeriodSelector
          idPrefix="comparison"
          presets={PERIOD_PRESETS}
          preset={preset}
          period={period}
          onPreset={(p) => {
            if (p === 'custom') return;
            const next = presetPeriod(p, today);
            setSearch({ from: next.from, to: next.to });
          }}
          onPeriod={(p) => setSearch({ from: p.from, to: p.to })}
        />
        <Field className="lg:max-w-40">
          <FieldLabel htmlFor="comparison-years">{t('dashboard:comparison.yearsBack')}</FieldLabel>
          <Select
            value={String(yearsBack)}
            onValueChange={(v) => setSearch({ years_back: v === '1' ? undefined : v })}
          >
            <SelectTrigger id="comparison-years" className="min-h-11 w-full sm:min-h-9">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {Array.from({ length: MAX_YEARS_BACK }, (_, i) => i + 1).map((n) => (
                <SelectItem key={n} value={String(n)}>
                  {t('dashboard:comparison.yearsBackOption', { count: n })}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
        <Field className="lg:max-w-44">
          <FieldLabel htmlFor="comparison-granularity">
            {t('dashboard:comparison.granularity.label')}
          </FieldLabel>
          <Select
            value={search.granularity ?? AUTO}
            onValueChange={(v) => setSearch({ granularity: v === AUTO ? undefined : v })}
          >
            <SelectTrigger id="comparison-granularity" className="min-h-11 w-full sm:min-h-9">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {[AUTO, ...GRANULARITIES].map((g) => (
                <SelectItem key={g} value={g}>
                  {t(`dashboard:comparison.granularity.${g}`)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
        <div className="flex flex-col gap-2">
          <span className="text-sm font-medium" id="comparison-align-label">
            {t('dashboard:align.label')}
          </span>
          <ToggleGroup
            type="single"
            variant="outline"
            value={align}
            onValueChange={(v) => v && setSearch({ align: v === 'date' ? undefined : v })}
            aria-labelledby="comparison-align-label"
          >
            {ALIGNS.map((a) => (
              <ToggleGroupItem key={a} value={a} className="min-h-11 px-3 sm:min-h-9">
                {t(`dashboard:align.${a}`)}
              </ToggleGroupItem>
            ))}
          </ToggleGroup>
        </div>
      </div>
      <div className="flex flex-wrap gap-2">
        <Button
          variant="outline"
          className="min-h-11 sm:min-h-9"
          disabled={!ready || file.isPending}
          onClick={() => download('csv')}
        >
          <DownloadIcon />
          {t('dashboard:comparison.exportCsv')}
        </Button>
        <Button
          variant="outline"
          className="min-h-11 sm:min-h-9"
          disabled={!ready || file.isPending}
          onClick={() => download('xlsx')}
        >
          <DownloadIcon />
          {t('dashboard:comparison.exportXlsx')}
        </Button>
      </div>
      {file.isError ? <ProblemAlert error={file.error} /> : null}
      {!ready ? null : comparison.isPending ? (
        <LoadingState rows={6} />
      ) : comparison.isError ? (
        <QueryErrorState error={comparison.error} onRetry={() => void comparison.refetch()} />
      ) : (
        <ComparisonBody result={comparison.data} />
      )}
    </div>
  );
}

function ComparisonBody({ result }: { result: ComparisonResult }) {
  const { t, i18n } = useT();
  const fmt = (v: number | null | undefined) => formatReportValue(v, result.unit, i18n.language);
  const rows = comparisonRows(result);
  const label = (b: string) => bucketLabel(b, result.granularity, i18n.language);
  const config: ChartConfig = {
    current: { ...chartConfig.current, label: t('dashboard:comparison.current') },
    previous: { ...chartConfig.previous, label: t('dashboard:comparison.previous') },
  };
  const range = (p: { from: string; to: string }) =>
    t('dashboard:comparison.range', { from: formatIsoDate(p.from), to: formatIsoDate(p.to) });
  return (
    <>
      <Card>
        <CardHeader>
          <CardTitle>{t('dashboard:comparison.chartTitle')}</CardTitle>
          <CardDescription>
            {range(result.current)} · {range(result.previous)}
          </CardDescription>
        </CardHeader>
        <CardContent>
          {rows.length === 0 ? (
            <p className="text-muted-foreground text-sm">{t('dashboard:comparison.noData')}</p>
          ) : (
            <ChartContainer
              config={config}
              className="aspect-auto h-64 w-full"
              data-testid="comparison-chart"
            >
              <LineChart data={rows} margin={{ left: 8, right: 8 }}>
                <CartesianGrid vertical={false} />
                <XAxis
                  dataKey="bucket"
                  tickLine={false}
                  axisLine={false}
                  minTickGap={24}
                  tickFormatter={label}
                />
                <YAxis hide />
                <ChartTooltip
                  content={
                    <ChartTooltipContent
                      labelFormatter={(_, payload) =>
                        label(String(payload?.[0]?.payload?.bucket ?? ''))
                      }
                      formatter={(value, name) => (
                        <div className="flex w-full justify-between gap-4">
                          <span className="text-muted-foreground">
                            {config[String(name)]?.label}
                          </span>
                          <span className="font-mono tabular-nums">
                            {fmt(typeof value === 'number' ? value : null)}
                          </span>
                        </div>
                      )}
                    />
                  }
                />
                <ChartLegend content={<ChartLegendContent />} />
                <Line
                  dataKey="previous"
                  type="monotone"
                  stroke="var(--color-previous)"
                  strokeDasharray="4 4"
                  dot={false}
                  isAnimationActive={false}
                />
                <Line
                  dataKey="current"
                  type="monotone"
                  stroke="var(--color-current)"
                  strokeWidth={2}
                  dot={false}
                  isAnimationActive={false}
                />
              </LineChart>
            </ChartContainer>
          )}
        </CardContent>
      </Card>
      <Table data-testid="comparison-table">
        <TableHeader>
          <TableRow>
            <TableHead>{t('dashboard:comparison.bucket')}</TableHead>
            <TableHead className="text-right">{t('dashboard:comparison.current')}</TableHead>
            <TableHead className="text-right">{t('dashboard:comparison.previous')}</TableHead>
            <TableHead className="text-right">{t('dashboard:comparison.delta')}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.map((r) => (
            <TableRow key={r.bucket} data-testid="comparison-row">
              <TableCell>{label(r.bucket)}</TableCell>
              <TableCell className="text-right tabular-nums">{fmt(r.current)}</TableCell>
              <TableCell className="text-right tabular-nums">{fmt(r.previous)}</TableCell>
              <TableCell className="text-right tabular-nums">{fmt(r.delta)}</TableCell>
            </TableRow>
          ))}
        </TableBody>
        <TableFooter>
          <TableRow data-testid="comparison-total">
            <TableCell>{t('dashboard:comparison.total')}</TableCell>
            <TableCell className="text-right tabular-nums">{fmt(result.current.value)}</TableCell>
            <TableCell className="text-right tabular-nums">{fmt(result.previous.value)}</TableCell>
            <TableCell className="text-right tabular-nums">
              {fmt(result.delta)}
              {result.delta_bp !== null && result.delta_bp !== undefined
                ? ` (${formatBp(result.delta_bp, i18n.language)})`
                : ''}
            </TableCell>
          </TableRow>
        </TableFooter>
      </Table>
    </>
  );
}
