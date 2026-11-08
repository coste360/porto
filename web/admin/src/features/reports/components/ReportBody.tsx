import { ChevronLeftIcon, ChevronRightIcon, InfoIcon } from 'lucide-react';
import { Bar, BarChart, CartesianGrid, XAxis, YAxis } from 'recharts';

import type { AnyReport, ReportRef, WasteSuggestion } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import type { ReportValueUnit } from '@porto/shared/lib/format';
import { Alert, AlertDescription } from '@porto/shared/ui/alert';
import { Button } from '@porto/shared/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@porto/shared/ui/card';
import {
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig,
} from '@porto/shared/ui/chart';
import { ScrollArea, ScrollBar } from '@porto/shared/ui/scroll-area';
import {
  Table,
  TableBody,
  TableCell,
  TableFooter,
  TableHead,
  TableHeader,
  TableRow,
} from '@porto/shared/ui/table';
import { cn } from '@porto/shared/lib/utils';

import { fieldOf, type ColumnDef, type ReportDef, type TotalDef } from '../lib/catalog';
import { formatCell, isNumeric, totalRow, unitOf, type Row } from '../lib/cells';
import { RefLink } from './RefLink';

type BodyProps = {
  def: ReportDef;
  data: AnyReport;
  section: string;
  marinaId: string;
  onPage: (page: number) => void;
};

/** A report result: summary cards, notes, chart and groups, suggestions (waste), records with the Totale row. */
export function ReportBody({ def, data, section, marinaId, onPage }: BodyProps) {
  const totals = data.totals as unknown as Row;
  const unit = unitOf(totals);
  const groups = (data.groups ?? []) as unknown as Row[];
  return (
    <div className="flex flex-col gap-4">
      <TotalsCards defs={def.totals} totals={totals} unit={unit} />
      <Notes notes={data.notes ?? []} />
      {groups.length > 0 ? <GroupsCard def={def} groups={groups} unit={unit} /> : null}
      {'suggestions' in data && data.suggestions.length > 0 ? (
        <SuggestionsCard suggestions={data.suggestions} />
      ) : null}
      <RecordsTable
        columns={def.columns(section)}
        records={data.records as unknown as Row[]}
        totals={totals}
        unit={unit}
        marinaId={marinaId}
      />
      <Pager
        page={data.page.page}
        pageSize={data.page.page_size}
        total={data.page.total_records}
        onPage={onPage}
      />
    </div>
  );
}

function useFormat(unit: ReportValueUnit) {
  const { t, i18n } = useT();
  return (value: unknown, d: { kind: ColumnDef['kind']; values?: string }) =>
    formatCell(value, d.kind, { t, locale: i18n.language, unit, values: d.values });
}

function TotalsCards({
  defs,
  totals,
  unit,
}: {
  defs: readonly TotalDef[];
  totals: Row;
  unit: ReportValueUnit;
}) {
  const { t } = useT();
  const fmt = useFormat(unit);
  return (
    <div className="grid grid-cols-2 gap-3 lg:grid-cols-4" data-testid="report-totals">
      {defs.map((d) => (
        <Card key={d.key} className="gap-1 py-4">
          <CardHeader className="px-4">
            <CardDescription>{t(`reports:columns.${d.key}`)}</CardDescription>
          </CardHeader>
          <CardContent className="px-4">
            <p
              className="text-lg font-semibold tabular-nums sm:text-2xl"
              data-testid={`report-total-${d.key}`}
            >
              {fmt(totals[d.key], d)}
            </p>
          </CardContent>
        </Card>
      ))}
    </div>
  );
}

function Notes({ notes }: { notes: { code: string; count: number }[] }) {
  const { t } = useT();
  if (notes.length === 0) return null;
  return (
    <Alert>
      <InfoIcon />
      <AlertDescription>
        <ul className="list-disc pl-4">
          {notes.map((n) => (
            <li key={n.code}>
              {t(`reports:notes.${n.code}`, {
                count: n.count,
                defaultValue: `${n.code}: ${n.count}`,
              })}
            </li>
          ))}
        </ul>
      </AlertDescription>
    </Alert>
  );
}

/** A group value: from its totals, or from the group itself (revenue classes). */
function groupValue(g: Row, key: string): unknown {
  const totals = g.totals as Row | undefined;
  return totals ? totals[key] : g[key];
}

function GroupsCard({
  def,
  groups,
  unit,
}: {
  def: ReportDef;
  groups: Row[];
  unit: ReportValueUnit;
}) {
  const { t } = useT();
  const fmt = useFormat(unit);
  const isClasses = def.key === 'revenue-classes';
  const cols: TotalDef[] = isClasses
    ? [
        { key: 'net_cents', kind: 'cents' },
        { key: 'share_bp', kind: 'bp' },
      ]
    : [...def.totals];
  const chart = def.chart;
  const chartData = chart
    ? groups.map((g) => ({
        label: String(g.label || g.key),
        value: Number(groupValue(g, chart.key) ?? 0),
      }))
    : [];
  const config: ChartConfig = {
    value: { label: chart ? t(`reports:columns.${chart.key}`) : '', color: 'var(--chart-1)' },
  };
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('reports:groups.title')}</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {chart ? (
          <ChartContainer
            config={config}
            className="aspect-auto h-56 w-full"
            data-testid="report-chart"
          >
            <BarChart data={chartData} margin={{ left: 8, right: 8 }}>
              <CartesianGrid vertical={false} />
              <XAxis dataKey="label" tickLine={false} axisLine={false} minTickGap={16} />
              <YAxis hide />
              <ChartTooltip
                content={
                  <ChartTooltipContent
                    formatter={(value) => (
                      <span className="font-mono tabular-nums">{fmt(value, chart)}</span>
                    )}
                  />
                }
              />
              <Bar dataKey="value" fill="var(--color-value)" radius={4} isAnimationActive={false} />
            </BarChart>
          </ChartContainer>
        ) : null}
        <ScrollArea className="w-full">
          <Table data-testid="report-groups">
            <TableHeader>
              <TableRow>
                <TableHead>{t('reports:groups.group')}</TableHead>
                {cols.map((c) => (
                  <TableHead key={c.key} className="text-right">
                    {t(`reports:columns.${c.key}`)}
                  </TableHead>
                ))}
              </TableRow>
            </TableHeader>
            <TableBody>
              {groups.map((g) => (
                <TableRow key={String(g.key)}>
                  <TableCell>{String(g.label || g.key)}</TableCell>
                  {cols.map((c) => (
                    <TableCell key={c.key} className="text-right tabular-nums">
                      {fmt(groupValue(g, c.key), c)}
                    </TableCell>
                  ))}
                </TableRow>
              ))}
            </TableBody>
          </Table>
          <ScrollBar orientation="horizontal" />
        </ScrollArea>
      </CardContent>
    </Card>
  );
}

function SuggestionsCard({ suggestions }: { suggestions: WasteSuggestion[] }) {
  const { t, i18n } = useT();
  const pct = (bp: number) => formatCell(bp, 'bp', { t, locale: i18n.language, unit: 'bp' });
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('reports:suggestions.title')}</CardTitle>
        <CardDescription>{t('reports:suggestions.description')}</CardDescription>
      </CardHeader>
      <CardContent>
        <ScrollArea className="w-full">
          <Table data-testid="report-suggestions">
            <TableHeader>
              <TableRow>
                <TableHead>{t('reports:columns.suggestion_kind')}</TableHead>
                <TableHead>{t('reports:columns.from_berths')}</TableHead>
                <TableHead>{t('reports:columns.to_berths')}</TableHead>
                <TableHead className="text-right">{t('reports:columns.waste_before_bp')}</TableHead>
                <TableHead className="text-right">{t('reports:columns.waste_after_bp')}</TableHead>
                <TableHead className="text-right">{t('reports:columns.gain_cm2_nights')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {suggestions.map((s) => (
                <TableRow key={s.allocation_ids.join('-')}>
                  <TableCell>{t(`reports:values.suggestionKind.${s.kind}`)}</TableCell>
                  <TableCell>{s.from_berths.map((b) => b.code).join(', ')}</TableCell>
                  <TableCell>{s.to_berths.map((b) => b.code).join(', ')}</TableCell>
                  <TableCell className="text-right tabular-nums">
                    {pct(s.waste_before_bp)}
                  </TableCell>
                  <TableCell className="text-right tabular-nums">{pct(s.waste_after_bp)}</TableCell>
                  <TableCell className="text-right tabular-nums">
                    {formatCell(s.gain_cm2_nights, 'cm2', {
                      t,
                      locale: i18n.language,
                      unit: 'cm2',
                    })}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
          <ScrollBar orientation="horizontal" />
        </ScrollArea>
      </CardContent>
    </Card>
  );
}

function RecordsTable({
  columns,
  records,
  totals,
  unit,
  marinaId,
}: {
  columns: readonly ColumnDef[];
  records: Row[];
  totals: Row;
  unit: ReportValueUnit;
  marinaId: string;
}) {
  const { t } = useT();
  const fmt = useFormat(unit);
  const total = totalRow(columns, totals);
  return (
    <ScrollArea className="w-full rounded-md border">
      <Table data-testid="report-records">
        <TableHeader>
          <TableRow>
            {columns.map((c) => (
              <TableHead key={c.key} className={cn(isNumeric(c.kind) && 'text-right')}>
                {t(`reports:columns.${c.key}`)}
              </TableHead>
            ))}
          </TableRow>
        </TableHeader>
        <TableBody>
          {records.length === 0 ? (
            <TableRow>
              <TableCell colSpan={columns.length} className="text-muted-foreground">
                {t('reports:records.empty')}
              </TableCell>
            </TableRow>
          ) : (
            records.map((r, i) => (
              <TableRow key={i} data-testid="report-record">
                {columns.map((c, ci) => {
                  const text = fmt(fieldOf(r, c.path ?? c.key), c);
                  return (
                    <TableCell
                      key={c.key}
                      className={cn(
                        'whitespace-nowrap',
                        isNumeric(c.kind) && 'text-right tabular-nums',
                      )}
                    >
                      {ci === 0 ? (
                        <RefLink marinaId={marinaId} refTo={r.ref as ReportRef | undefined}>
                          {text || (r.ref as ReportRef | undefined)?.label || '—'}
                        </RefLink>
                      ) : (
                        text
                      )}
                    </TableCell>
                  );
                })}
              </TableRow>
            ))
          )}
        </TableBody>
        <TableFooter>
          <TableRow data-testid="report-total-row">
            {columns.map((c, i) => (
              <TableCell
                key={c.key}
                className={cn('whitespace-nowrap', isNumeric(c.kind) && 'text-right tabular-nums')}
                data-testid={c.total ? `report-total-cell-${c.key}` : undefined}
              >
                {i === 0 && total[0] === undefined
                  ? t('reports:total')
                  : total[i] === undefined
                    ? ''
                    : fmt(total[i], c)}
              </TableCell>
            ))}
          </TableRow>
        </TableFooter>
      </Table>
      <ScrollBar orientation="horizontal" />
    </ScrollArea>
  );
}

function Pager({
  page,
  pageSize,
  total,
  onPage,
}: {
  page: number;
  pageSize: number;
  total: number;
  onPage: (page: number) => void;
}) {
  const { t } = useT();
  const pages = Math.max(1, Math.ceil(total / Math.max(1, pageSize)));
  if (pages <= 1) {
    return (
      <p className="text-muted-foreground text-sm">
        {t('reports:records.count', { count: total })}
      </p>
    );
  }
  return (
    <div className="flex flex-wrap items-center justify-between gap-2" data-testid="report-pager">
      <p className="text-muted-foreground text-sm">
        {t('reports:records.page', { page, pages, count: total })}
      </p>
      <div className="flex gap-2">
        <Button
          variant="outline"
          className="min-h-11 sm:min-h-9"
          disabled={page <= 1}
          onClick={() => onPage(page - 1)}
        >
          <ChevronLeftIcon />
          {t('reports:records.previous')}
        </Button>
        <Button
          variant="outline"
          className="min-h-11 sm:min-h-9"
          disabled={page >= pages}
          onClick={() => onPage(page + 1)}
        >
          {t('reports:records.next')}
          <ChevronRightIcon />
        </Button>
      </div>
    </div>
  );
}
